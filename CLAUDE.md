# Working rules for this repo

Applies to humans and AI agents alike.

## Every PR includes a new plan

1. Copy `plans/TEMPLATE.md` to `plans/NNNN-short-slug.md` (next unused number).
2. Fill it in: why, what changes, how it is tested, risks. Update diagrams if the architecture changed.
3. **Link it from `README.md`** in the "Plans" section. CI fails if any file in `plans/` is not linked.
4. Do not edit old plans to rewrite history. If a decision is reversed, say so in the new plan and link the old one.

`test/plans.sh` enforces naming, README links, and (in PRs) that a new plan was added.

## Tests

- Unit-test coverage of `internal/...` must stay **≥ 90%** (`make cover`; CI enforces it). Never lower the bar; new code ships with its tests.
- A bug fix starts with a failing regression test (unit if possible, e2e if it needs a real API server or RBAC).
- UI changes (`internal/server/ui.html`) need a case in `test/ui/ui.test.js` (`make ui-test`, jsdom + node:test; CI job `ui`).
- Chart changes need an assertion in `test/chart.sh`. Behaviour that touches Kubernetes (RBAC, PVCs, GC, rollouts) needs a check in `test/e2e.sh`, because the fake client cannot catch those.
- Run `make check` before pushing; run `make e2e` (against a throwaway cluster, never a shared one) for anything touching reconcile, RBAC or the chart.
- See `plans/0002-pr-review-and-test-gates.md` for the full review checklist.

## CI

- `ci.yaml` runs unit (90% gate), ui, chart, plans and e2e jobs on **every push to any branch** while the project is in early development. `pr-plan.yaml` runs only on pull requests and enforces "adds a new plan". To move to PR-only checks, follow `plans/0004-ci-on-every-commit.md`.

## Tooling

- No local Go toolchain is assumed: the Makefile runs Go inside Docker (`golang:1.26`).
- `HermesAgent` is handled as unstructured; the CRD YAML in `charts/hermes-operator/crds/` is the only schema. Keep `internal/agent/types.go`, the CRD and `test/chart.sh`'s CRD check in sync.
- Do not run e2e or experiments against a real cluster context. Use a throwaway k3s/kind cluster.
