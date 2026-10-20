#!/usr/bin/env bash
# Renders the chart with different values and asserts on the output.
# Requires: helm. Run from the repo root: test/chart.sh
set -euo pipefail
CHART=charts/hermes-operator
fail=0

render() { helm template r "$CHART" -n hermes "$@"; }
ok()   { echo "ok   - $1"; }
bad()  { echo "FAIL - $1"; fail=1; }
has()  { local name=$1 pattern=$2; shift 2; if render "$@" | grep -Eq -- "$pattern"; then ok "$name"; else bad "$name"; fi; }
hasnt(){ local name=$1 pattern=$2; shift 2; if render "$@" | grep -Eq -- "$pattern"; then bad "$name"; else ok "$name"; fi; }

helm lint "$CHART" >/dev/null && ok "helm lint" || bad "helm lint"

# defaults: namespaced, least privilege
has   "default: Role (not ClusterRole)"        '^kind: Role$'
hasnt "default: no ClusterRole"                '^kind: ClusterRole'
has   "default: watches release namespace"     '--watch-namespace=hermes$'
has   "default: agents created in release ns"  '--agent-namespace=hermes$'
hasnt "default: no ingress"                    '^kind: Ingress'
hasnt "default: no agents"                     '^kind: HermesAgent'
has   "default: agent cpu request in defaults" 'cpu: 500m'
has   "default: agent memory request"          'memory: 1Gi'
has   "operator runs non-root, read-only fs"   'readOnlyRootFilesystem: true'

# cluster-wide
has   "all namespaces: ClusterRole"            '^kind: ClusterRole$' --set watchNamespace='*'
has   "all namespaces: empty watch flag"       '--watch-namespace=$' --set watchNamespace='*'
has   "all namespaces: agents still in release ns" '--agent-namespace=hermes$' --set watchNamespace='*'

# specific namespace
has   "explicit namespace: watch + agent ns"   '--agent-namespace=team-a$' --set watchNamespace=team-a
has   "explicit namespace: Role in that ns"    'namespace: team-a' --set watchNamespace=team-a
has   "agentNamespace override"                '--agent-namespace=agents$' --set agentNamespace=agents

# resource overrides flow into the defaults ConfigMap the operator reads
has   "override requests"  'cpu: "4"' --set-string agentDefaults.resources.requests.cpu=4
has   "override limits"    'cpu: "8"' --set-string agentDefaults.resources.limits.cpu=8
has   "shared key secret"  'envFromSecret: my-keys' --set agentDefaults.envFromSecret=my-keys

# model providers for the UI dropdown (built-ins live in the binary; values add/override)
hasnt "providers: none by default"        'providers.yaml'
has   "providers: file written when set"  'providers.yaml: \|' --set 'providers[0].id=nvidia' --set 'providers[0].modelsURL=https://x/v1/models'
has   "providers: flag passed when set"   '--providers-file=/etc/hermes-operator/providers.yaml' --set 'providers[0].id=nvidia' --set 'providers[0].modelsURL=https://x/v1/models'
has   "providers: entry rendered"         'modelsURL: https://x/v1/models' --set 'providers[0].id=nvidia' --set 'providers[0].modelsURL=https://x/v1/models'
has   "providers: key help rendered"    'keyURL: https://example.com/keys' --set 'providers[0].id=nvidia' --set 'providers[0].keyURL=https://example.com/keys' --set 'providers[0].keySteps[0]=Sign in'

# declarative agents
has   "agents: renders HermesAgent"   '^kind: HermesAgent$' --set agents.bot.model.provider=openrouter
has   "agents: spec passthrough"      'provider: openrouter' --set agents.bot.model.provider=openrouter

# ingress
has   "ingress enabled"  '^kind: Ingress$' --set ingress.enabled=true
has   "ingress host"     'host: hermes.local' --set ingress.enabled=true --set ingress.host=hermes.local

# defaults change must roll the operator
a=$(render | grep checksum/defaults)
b=$(render --set agentDefaults.image=other:1 | grep checksum/defaults)
[ "$a" != "$b" ] && ok "defaults checksum rolls the operator" || bad "defaults checksum rolls the operator"

# CRD stays in sync with what the Go types accept
grep -q 'name: hermesagents.hermesoperator.io' "$CHART/crds/hermesagents.yaml" && ok "CRD name" || bad "CRD name"
for f in image model config soul env envFrom resources storage dashboard suspended retainData; do
  grep -Eq "^                $f:" "$CHART/crds/hermesagents.yaml" && ok "CRD spec.$f" || bad "CRD spec.$f"
done

exit $fail
