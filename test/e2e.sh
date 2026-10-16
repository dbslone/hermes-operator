#!/usr/bin/env bash
# End-to-end test against whatever cluster KUBECONFIG points at (kind, k3s, ...).
# The operator image must already be available to the cluster.
#
#   IMG=hermes-operator:e2e test/e2e.sh
#
# Agents use a tiny image by default so no real Hermes image is pulled: we
# assert on what the operator creates, not on Hermes itself.
# Set AGENT_IMAGE=nousresearch/hermes-agent:stable for a full run.
set -euo pipefail

IMG=${IMG:?set IMG to the operator image, e.g. hermes-operator:e2e}
AGENT_IMAGE=${AGENT_IMAGE:-busybox:1.37}
NS=${NS:-hermes-e2e}
REL=hermes-operator
PORT=${PORT:-18090}
CHART="$(cd "$(dirname "$0")/.." && pwd)/charts/hermes-operator"
K="kubectl -n $NS"
pf_pid=""

fail=0
ok()  { echo "ok   - $1"; }
bad() { echo "FAIL - $1"; fail=1; }
check() { local name=$1; shift; if "$@" >/dev/null 2>&1; then ok "$name"; else bad "$name"; fi; }

# retry <seconds> <cmd...>: run until it succeeds
retry() { local t=$1; shift; local end=$((SECONDS+t)); until "$@" >/dev/null 2>&1; do [ $SECONDS -ge $end ] && return 1; sleep 2; done; }
eq() { [ "$(eval "$1")" = "$2" ]; }

cleanup() {
  [ -n "$pf_pid" ] && kill "$pf_pid" 2>/dev/null || true
  if [ $fail -ne 0 ]; then
    echo "---- diagnostics"; $K get all,hagent,pvc,secret 2>&1 || true
    $K logs deploy/$REL --tail=50 2>&1 || true
  fi
  helm uninstall $REL -n $NS >/dev/null 2>&1 || true
  $K delete hagent --all --wait=false >/dev/null 2>&1 || true
  kubectl delete ns $NS --wait=false >/dev/null 2>&1 || true
  kubectl delete crd hermesagents.hermesoperator.io --wait=false >/dev/null 2>&1 || true
}
trap cleanup EXIT

kubectl create ns $NS >/dev/null 2>&1 || true
helm install $REL "$CHART" -n $NS \
  --set image.repository="${IMG%:*}" --set image.tag="${IMG##*:}" --set image.pullPolicy=IfNotPresent \
  --set agentDefaults.image="$AGENT_IMAGE" \
  --set agents.declared.model.provider=openrouter \
  --set agents.declared.model.name=test-model \
  --wait --timeout 3m
ok "chart installed, operator ready"

$K port-forward svc/$REL $PORT:80 >/dev/null 2>&1 & pf_pid=$!
retry 30 curl -sf localhost:$PORT/healthz || { bad "operator reachable"; exit 1; }
API=localhost:$PORT

jp() { $K get "$@" 2>/dev/null; }

# --- declared in Helm values -------------------------------------------------
check "helm-declared agent gets a statefulset" retry 60 jp sts/declared
check "helm-declared agent has a service"      jp svc/declared

# --- UI / REST ---------------------------------------------------------------
check "UI served" curl -sf $API/ -o /dev/null
curl -sf -XPOST $API/api/agents -d '{"name":"rest-bot","soul":"hello","model":{"provider":"openrouter","name":"m"},"env":{"OPENROUTER_API_KEY":"sk-e2e"}}' >/dev/null && ok "REST create" || bad "REST create"
check "statefulset created"      retry 60 jp sts/rest-bot
check "service created"          jp svc/rest-bot
check "api key secret generated" retry 30 jp secret/rest-bot-api
check "env secret created"       jp secret/rest-bot-env
check "pvc created"              retry 60 jp pvc/data-rest-bot-0
check "default cpu request applied"    eq "$K get sts rest-bot -o jsonpath='{.spec.template.spec.containers[0].resources.requests.cpu}'" 500m
check "default memory request applied" eq "$K get sts rest-bot -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}'" 1Gi
check "no cpu limit by default"        eq "$K get sts rest-bot -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}'" ""
check "API key not stored in the CR"   bash -c "! $K get hagent rest-bot -o yaml | grep -q sk-e2e"
check "init container seeded volume"   retry 120 eq "$K get pod rest-bot-0 -o jsonpath='{.status.initContainerStatuses[0].state.terminated.exitCode}'" 0
check "status.phase populated"         retry 30 bash -c "[ -n \"\$($K get hagent rest-bot -o jsonpath='{.status.phase}')\" ]"
check "listed in REST API"             bash -c "curl -sf $API/api/agents | grep -q rest-bot"
check "duplicate create rejected (409)" eq "curl -s -o /dev/null -w '%{http_code}' -XPOST $API/api/agents -d '{\"name\":\"rest-bot\"}'" 409

# --- MCP ---------------------------------------------------------------------
mcp() { curl -sf -XPOST $API/mcp -d "$1"; }
mcp '{"jsonrpc":"2.0","id":1,"method":"initialize"}' | grep -q hermes-operator && ok "mcp initialize" || bad "mcp initialize"
mcp '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"create_agent","arguments":{"name":"mcp-bot","provider":"anthropic","model":"m"}}}' | grep -q '"isError":false' && ok "mcp create_agent" || bad "mcp create_agent"
check "mcp-created agent has a statefulset" retry 60 jp sts/mcp-bot

# --- resource override (CRD, as a user would kubectl apply) ------------------
cat <<YAML | $K apply -f - >/dev/null
apiVersion: hermesoperator.io/v1alpha1
kind: HermesAgent
metadata: {name: big}
spec:
  resources:
    requests: {cpu: "100m", memory: 64Mi}
    limits: {cpu: "2", memory: 2Gi}
YAML
check "per-agent cpu request override" retry 60 eq "$K get sts big -o jsonpath='{.spec.template.spec.containers[0].resources.requests.cpu}'" 100m
check "per-agent cpu limit override"   eq "$K get sts big -o jsonpath='{.spec.template.spec.containers[0].resources.limits.cpu}'" 2

# --- spec changes propagate --------------------------------------------------
before=$($K get sts rest-bot -o jsonpath='{.spec.template.metadata.annotations.hermesoperator\.io/seed-hash}')
$K patch hagent rest-bot --type merge -p '{"spec":{"soul":"changed"}}' >/dev/null
check "soul change updates seed configmap" retry 30 eq "$K get cm rest-bot-seed -o jsonpath='{.data.SOUL\\.md}'" changed
after=$($K get sts rest-bot -o jsonpath='{.spec.template.metadata.annotations.hermesoperator\.io/seed-hash}')
[ "$before" != "$after" ] && ok "soul change rolls the pod (hash changed)" || bad "soul change rolls the pod"

$K patch hagent rest-bot --type merge -p '{"spec":{"suspended":true}}' >/dev/null
check "suspend scales to zero" retry 30 eq "$K get sts rest-bot -o jsonpath='{.spec.replicas}'" 0
check "suspend keeps the pvc"  jp pvc/data-rest-bot-0
check "phase is Suspended"     retry 30 eq "$K get hagent rest-bot -o jsonpath='{.status.phase}'" Suspended

# --- operator restart must not disturb running agents ------------------------
key_before=$($K get secret rest-bot-api -o jsonpath='{.data.api-key}')
$K rollout restart deploy/$REL >/dev/null && $K rollout status deploy/$REL --timeout=120s >/dev/null
sleep 5
check "api key survives operator restart" eq "$K get secret rest-bot-api -o jsonpath='{.data.api-key}'" "$key_before"

# --- delete cleans up everything ---------------------------------------------
pkill -f "port-forward svc/$REL" 2>/dev/null || true
$K port-forward svc/$REL $PORT:80 >/dev/null 2>&1 & pf_pid=$!
retry 30 curl -sf $API/healthz
curl -sf -XDELETE $API/api/agents/rest-bot >/dev/null && ok "REST delete" || bad "REST delete"
gone() { ! $K get "$@" >/dev/null 2>&1; }
check "statefulset garbage collected" retry 90 gone sts/rest-bot
check "service garbage collected"     retry 60 gone svc/rest-bot
check "secrets garbage collected"     retry 60 bash -c "! $K get secret -o name | grep -q rest-bot"
check "pvc deleted with the agent"    retry 90 gone pvc/data-rest-bot-0

mcp '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"delete_agent","arguments":{"name":"mcp-bot"}}}' | grep -q '"isError":false' && ok "mcp delete_agent" || bad "mcp delete_agent"
check "mcp-deleted agent removed" retry 90 gone sts/mcp-bot

exit $fail
