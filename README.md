# hermes-operator

Spin up [Hermes agents](https://github.com/NousResearch/hermes-agent) on Kubernetes in seconds — from a web UI, `kubectl`, Helm values, or **another agent over MCP**.

- **No database.** Each agent is a `HermesAgent` custom resource; Kubernetes is the only state.
- **One small pod.** The operator, UI, REST API and MCP endpoint are a single Go binary (~48Mi).
- **Sensible resources.** Every agent gets CPU/memory requests out of the box; raise them chart-wide or per agent.
- **Drop-in.** Install as its own chart or add it as a dependency of your existing chart.

> ⚠️ **No authentication.** The UI, REST API and MCP endpoint let anyone who can reach them create agents (and agents run with your model API keys). The service is `ClusterIP` and ingress is off by default. If you expose it, put an authenticating proxy (e.g. oauth2-proxy) in front.

## Install

**Requirements:** a Kubernetes cluster with a default StorageClass (each agent gets a PVC), and Helm 3.8+ (OCI charts). No database, ingress controller or cert-manager is needed. The chart and the image are public: `oci://ghcr.io/dbslone/charts/hermes-operator` and `ghcr.io/dbslone/hermes-operator`, both tagged with the release version.

```sh
helm install hermes-operator oci://ghcr.io/dbslone/charts/hermes-operator \
  --version 0.0.7 -n hermes --create-namespace
kubectl -n hermes port-forward svc/hermes-operator 8080:80   # UI: http://localhost:8080
```

By default the operator watches, and creates agents in, **the namespace it is installed into**. The commands in this README use the release name `hermes-operator`; with a different release name the Service is called `<release>-hermes-operator`.

### Add to an existing chart

```yaml
# Chart.yaml
dependencies:
  - name: hermes-operator
    version: 0.0.7
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

Run `helm dependency update` after editing `Chart.yaml`. The operator pod runs in your release's namespace; to keep agents elsewhere, set where they live (the namespace **must already exist**, the chart does not create it):

```yaml
hermes-operator:
  watchNamespace: agents   # where the operator watches and has RBAC (Role in this namespace)
  agentNamespace: agents   # where UI/MCP/Helm-declared agents are created
```

If the namespace uses Istio, label it `istio-injection=enabled` before creating agents so they get a sidecar.

### Install with Argo CD

The chart ships a `CustomResourceDefinition`. Argo CD rejects a sync that contains a kind the Application's project does not allow (`CustomResourceDefinition is not permitted in project ...`) and applies nothing. Add it to the project's cluster-scoped allow-list first:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: AppProject
spec:
  clusterResourceWhitelist:
    - { group: apiextensions.k8s.io, kind: CustomResourceDefinition }
```

The `default` project already allows everything. Then point an Application at the OCI chart (`repoURL: ghcr.io/dbslone/charts`, `chart: hermes-operator`, `targetRevision: 0.0.7`, source type OCI) or add it as a dependency as above. With Helm directly there is nothing to configure: it installs the CRD on first install.

> Helm installs CRDs only on first install and never upgrades them. To upgrade the CRD run
> `kubectl apply -f charts/hermes-operator/crds/`. Uninstalling the chart leaves the CRD (and so your agents and their data) in place.

### Configuration

| Value | Default | Meaning |
| --- | --- | --- |
| `watchNamespace` | `""` | `""` release namespace (Role), `"*"` all namespaces (ClusterRole), or a namespace name |
| `agentNamespace` | `""` | Where UI/MCP/`agents:` agents are created. Defaults to the watched namespace |
| `agentDefaults.image` | `nousresearch/hermes-agent:stable` | Agent image |
| `agentDefaults.resources` | 500m / 1Gi request, 4Gi memory limit | Per-agent resources |
| `agentDefaults.storage.size` | `2Gi` | PVC per agent (`storageClassName: ""` = cluster default) |
| `agentDefaults.envFromSecret` | `""` | Secret whose keys become env vars (provider API keys) in every agent |
| `providers` | `[]` | Extra/overriding UI model providers (`id`, `label`, `modelsURL`, `auth`, `modelPrefix`, `runtimeProvider`, `keyEnv`, `keyURL`, `keySteps`); see below |
| `agents` | `{}` | Declare `HermesAgent`s in values |
| `image.repository` / `image.tag` | ghcr image / chart `appVersion` | Operator image |
| `service`, `ingress` | `ClusterIP`, off | **No authentication**; see the warning above |

## Creating agents

In the UI, **Provider** is a dropdown that opens on **Default**. **Model** is on the next line and is a dropdown filled live from the provider's own API (cached ten minutes), so newly released models appear without upgrading the operator. OpenRouter's list is public. **Anthropic** uses that same public list, kept to model ids that start with `anthropic/`, and saving an agent stores provider `openrouter` with that id. OpenRouter bills those requests. Save `OPENROUTER_API_KEY` under Advanced. A provider that needs an API key shows that field between Provider and Model, with the steps to create or retrieve the key; until the key is entered, Model stays disabled and the form says the API key is required. The key is saved on the agent. `Other…` on a loaded list, and a custom provider id, stay free text. kubectl, Helm, REST, and MCP still accept any provider id Hermes accepts, including `anthropic` with `ANTHROPIC_API_KEY`. Built in: OpenRouter, Anthropic, OpenAI (`openai-api`), DeepSeek, xAI, Fireworks. Add more, or change an endpoint or the key instructions, with chart values:

```yaml
providers:
  - id: nvidia                       # a provider id Hermes accepts in model.provider
    label: NVIDIA NIM
    modelsURL: https://integrate.api.nvidia.com/v1/models   # returns {"data":[{"id": ...}]}
    auth: bearer                     # none | bearer | anthropic
    keyEnv: NVIDIA_API_KEY
    keyURL: https://build.nvidia.com/    # link under the API key field; never fetched
    keySteps:
      - Sign in and open API keys.
      - Create a key and paste it in the API key field.
```

The browser never sends a models URL; lookups go only to the `modelsURL`s you configure.

| How | Example |
| --- | --- |
| UI | Open the UI → **+ New agent** |
| kubectl | `kubectl apply -f examples/agent.yaml` |
| Helm values | `agents:` map (above) |
| REST | `curl -XPOST $URL/api/agents -d '{"name":"bot","model":{"provider":"openrouter","name":"..."},"env":{"OPENROUTER_API_KEY":"..."}}'` |
| MCP | point any MCP client at `http://<release>-hermes-operator.<operator-ns>.svc/mcp` (`hermes-operator` when the release is named that) |

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

No local Go needed; everything runs in Docker. Run `make help` for all targets.

```sh
make check        # build + unit tests (>= 90% coverage gate) + UI tests + chart lint/tests + plans check
make ui-test      # just the jsdom tests for internal/server/ui.html (Docker, no local Node)
make e2e IMG=hermes-operator:e2e   # real install on a THROWAWAY cluster (k3s/kind) via $KUBECONFIG
make image IMG=ghcr.io/dbslone/hermes-operator:dev
```

Tests come in four layers (unit, UI, chart, end-to-end); see [Test coverage map](plans/0003-test-coverage-map.md). Contribution rules, including **"every PR adds a plan"**, are in [CLAUDE.md](CLAUDE.md).

## Plans

Design decisions, test strategy and review gates live in [`plans/`](plans/). Every PR adds a new plan (copy the template), and every plan must be linked here (CI checks this).

| Plan | What it covers |
| --- | --- |
| [0001 – How we got here](plans/0001-how-we-got-here.md) | Requirements, architecture diagrams, every technology choice and why, build history, testing, bugs found, known gaps |
| [0002 – PR review and test gates](plans/0002-pr-review-and-test-gates.md) | Review checklist, which tests each kind of change needs, CI gates, 90% coverage rule |
| [0003 – Test coverage map](plans/0003-test-coverage-map.md) | Every promise mapped to the tests that protect it; exclusions and gaps |
| [0004 – CI on every commit](plans/0004-ci-on-every-commit.md) | Why CI runs on every push for now, and the one-line switch to PR-only later |
| [0005 – First release 0.0.1](plans/0005-first-release-0-0-1.md) | How releases are cut (tag → image + chart), versions, and what to check after publishing |
| [0006 – Release 0.0.2](plans/0006-release-0-0-2.md) | Why 0.0.1 was never published, faster multi-arch build, image source label |
| [0007 – README install guide](plans/0007-readme-install-guide.md) | Install requirements, subchart/namespace split, Argo CD CRD allow-list, configuration table |
| [0008 – Dynamic provider and model dropdowns](plans/0008-dynamic-provider-and-model-dropdowns.md) | Live model lists, provider data, key handling, SSRF decision |
| [0009 – Model field is a real dropdown](plans/0009-model-select-dropdown.md) | Replaces the datalist with a select once models load; text fallback |
| [0010 – UI test runner](plans/0010-ui-test-runner.md) | jsdom tests for the embedded UI, `make ui-test`, CI job |
| [0011 – Model key wizard](plans/0011-model-key-wizard.md) | When a model list cannot be loaded, ask for an API key (with steps) and then which model |
| [0012 – Inline provider, key, and model](plans/0012-inline-provider-key.md) | One screen: Default provider, key between provider and model, model disabled until the key is entered |
| [0013 – Anthropic models from OpenRouter](plans/0013-anthropic-models-from-openrouter.md) | Claude model list comes from OpenRouter; the agent is saved as provider openrouter |
| [Template](plans/TEMPLATE.md) | Copy to `plans/NNNN-slug.md` for your PR |

## License

Apache-2.0
