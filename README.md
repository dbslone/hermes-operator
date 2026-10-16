# hermes-operator

Spin up [Hermes agents](https://github.com/NousResearch/hermes-agent) on Kubernetes in seconds — from a web UI, `kubectl`, Helm values, or **another agent over MCP**.

- **No database.** Each agent is a `HermesAgent` custom resource; Kubernetes is the only state.
- **One small pod.** The operator, UI, REST API and MCP endpoint are a single Go binary (~48Mi).
- **Sensible resources.** Every agent gets CPU/memory requests out of the box; raise them chart-wide or per agent.
- **Drop-in.** Install as its own chart or add it as a dependency of your existing chart.

> ⚠️ **No authentication.** The UI, REST API and MCP endpoint let anyone who can reach them create agents (and agents run with your model API keys). The service is `ClusterIP` and ingress is off by default. If you expose it, put an authenticating proxy (e.g. oauth2-proxy) in front.

## Install

```sh
helm install hermes-operator oci://ghcr.io/dbslone/charts/hermes-operator \
  -n hermes --create-namespace
kubectl -n hermes port-forward svc/hermes-operator 8080:80   # UI: http://localhost:8080
```

### Add to an existing chart

```yaml
# Chart.yaml
dependencies:
  - name: hermes-operator
    version: 0.1.0
    repository: oci://ghcr.io/dbslone/charts
```
```yaml
# values.yaml  (everything nested under the subchart name)
hermes-operator:
  agentDefaults:
    envFromSecret: my-llm-keys      # shared provider keys for all agents
  agents:                           # optional: declare agents right here
    research-bot:
      model: {provider: openrouter, name: nousresearch/hermes-4-405b}
      soul: You are a research assistant.
```

> Helm installs CRDs only on first install and never upgrades them. To upgrade the CRD run
> `kubectl apply -f charts/hermes-operator/crds/`.

## Creating agents

| How | Example |
| --- | --- |
| UI | Open the UI → **+ New agent** |
| kubectl | `kubectl apply -f examples/agent.yaml` |
| Helm values | `agents:` map (above) |
| REST | `curl -XPOST $URL/api/agents -d '{"name":"bot","model":{"provider":"openrouter","name":"..."},"env":{"OPENROUTER_API_KEY":"..."}}'` |
| MCP | point any MCP client at `http://hermes-operator.<ns>.svc/mcp` |

**MCP tools:** `list_agents`, `get_agent`, `create_agent`, `delete_agent` (stateless Streamable HTTP, JSON responses).
Claude Code: `claude mcp add --transport http hermes http://hermes-operator.hermes.svc/mcp`.

Skip pasting keys every time: create one Secret with your provider keys and set `agentDefaults.envFromSecret`.

Each agent exposes Hermes' OpenAI-compatible API at `http://<name>.<ns>.svc:8642`; the generated key is in Secret `<name>-api` (`api-key`).

## Resources

Defaults (`values.yaml`):

```yaml
agentDefaults:
  resources:
    requests: {cpu: 500m, memory: 1Gi}
    limits:   {memory: 4Gi}          # no CPU limit: agents burst when the node is idle
```

Big machine? Raise it for all agents via the chart, or for a single agent:

```yaml
spec:
  resources:
    requests: {cpu: "4", memory: 8Gi}
    limits:   {cpu: "8", memory: 16Gi}
```
Hermes recommends ≥ 2 GB if you use browser tools.

## Behaviour notes

- Each agent = StatefulSet (1 replica) + PVC (`/opt/data`) + Service + two Secrets/ConfigMap. Deleting the agent deletes its data unless `spec.retainData: true`. `spec.suspended: true` scales to zero and keeps data.
- `spec.soul` and `spec.config` are written to `SOUL.md` / `config.yaml` on the PVC. Edit those files inside the pod and your edits persist across restarts; change the value in the spec and the file is overwritten and the pod restarts. A hash marker on the volume tracks what was last applied.
- `spec.dashboard: true` also starts the Hermes dashboard on port 9119 (user `admin`, password in Secret `<name>-api`).
- Scope: `watchNamespace: ""` (release namespace, Role), `"*"` (all namespaces, ClusterRole) or a specific namespace. Agents created through UI/MCP go to `agentNamespace`.
- API keys are passed as environment variables; model and provider are Hermes settings, so they are written to `config.yaml` (`model.default`, `model.provider`, `model.base_url`). For anything else supply a full `spec.config`.

## Development

No local Go needed:

```sh
make build        # compile in Docker
make chart-lint
make image IMG=ghcr.io/you/hermes-operator:dev
```

## License

Apache-2.0
