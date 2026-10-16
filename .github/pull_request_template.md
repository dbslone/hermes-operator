## Summary
<!-- What and why, one or two sentences. -->

## Plan
- [ ] Added `plans/NNNN-….md` (copied from `plans/TEMPLATE.md`) **and linked it from README.md**

## Tests (see plans/0002-pr-review-and-test-gates.md)
- [ ] `make check` passes locally (coverage ≥ 90%)
- [ ] New/changed behaviour has a unit test; bug fixes have a regression test
- [ ] Chart changes are asserted in `test/chart.sh`
- [ ] Kubernetes-facing changes (RBAC, PVC, GC, rollout) are covered in `test/e2e.sh`, and `make e2e` passed on a throwaway cluster
- [ ] Docs/README updated if user-visible behaviour changed
