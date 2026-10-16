# 0007 – README install guide

- **Status:** done
- **Date:** 2026-10-16
- **PR:** docs
- **Related plans:** [0005 First release 0.0.1](0005-first-release-0-0-1.md) · [0006 Release 0.0.2](0006-release-0-0-2.md)

## Why

Deploying the operator as a subchart under Argo CD surfaced things the README did not say: the chart's CRD is rejected by an Argo project that does not allow `CustomResourceDefinition`, agents can live in a different namespace than the operator pod (which must already exist), and the Service name depends on the release name. Someone else installing it would have hit the same walls.

## What changes

- Install section: requirements, pinned `--version`, release-name note.
- Subchart section: `helm dependency update`, `watchNamespace`/`agentNamespace` split, Istio label hint.
- New Argo CD section with the project allow-list.
- Configuration table of the values that matter.
- MCP URL uses `<release>-hermes-operator`.

## How it is tested

| Behaviour | Layer | Check |
| --- | --- | --- |
| Plan named and linked | CI | `test/plans.sh` |
| Documented values exist | manual | compared with `charts/hermes-operator/values.yaml` |

- Coverage after this change: unchanged (docs only)
- Regression test added for any bug fixed: n/a

## Risks, rollout, rollback

Docs only; no release needed. The pinned `0.0.2` in examples must be bumped on each release.

## Follow-ups

- Add `helm install` smoke steps for the Argo example if CRD handling changes.
