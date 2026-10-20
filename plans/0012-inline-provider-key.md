# 0012 – Inline provider, key, and model

- **Status:** done
- **Date:** 2026-10-20
- **PR:** unreleased (0.0.6)
- **Related plans:** [0011 Model key wizard](0011-model-key-wizard.md)

## Why

The key wizard put Provider and Model on one row and moved the API key to a second screen. The create form should stay on one screen: a Default provider, the model on its own line, and the API key between them when that provider needs one.

## What changes

- Provider is its own line and opens on **Default** (`value=""`, selected). Providers follow, then `Other…`.
- Model is the next full-width line. It is a `<select>` of **Default**, the live model ids, and `Other…` once a list loads.
- When the selected provider has `needsKey`, an API key line (password, `keySteps`, `keyURL`) is shown between Provider and Model.
- An empty key disables Model, shows **API key is required**, sends no model request, and **Create** does not submit.
- A non-empty key is sent to `POST /api/models` after a short pause. Success enables the model select. A failed lookup leaves Model disabled and shows the error. The key is saved on the agent as `env[keyEnv]` and wins over the same name in the advanced box. A key already in that box is copied into the API key line.
- OpenRouter, Default, and `Other…` hide the API key line. The second-screen wizard is gone.
- Version 0.0.6 (Chart, MCP serverInfo, README).

## Design and alternatives

- **One screen** instead of the 0011 wizard: the key is a field, not a step.
- **Debounce the key** so a half-typed secret is not sent on every keystroke. An in-flight lookup is dropped when the key or provider changes.
- **`http:` and `https:` links only.** A `javascript:` `keyURL` is not turned into an href.

## How it is tested

| Behaviour | Layer | Test |
| --- | --- | --- |
| Default is selected; provider and model are not in one row | UI | `provider select opens on Default…` |
| OpenRouter lists models and hides the key line | UI | `choosing a provider turns model into a select…` |
| Empty key disables model, shows the error, blocks create, sends no lookup | UI | `a provider that needs a key shows the key line…` |
| Partial key waits; valid key enables the list and is saved over the advanced value | UI | `a partial key is not sent…`, `a valid key enables…` |
| Rejected key leaves model disabled; missing steps and unsafe links | UI | `a rejected key…`, `a keyed provider with no steps…` |

- Coverage after this change: unchanged Go (99.0% before this UI-only behaviour change; must be ≥ 90%)
- Regression test added for any bug fixed: n/a

## Risks, rollout, rollback

- The API key is still sent to the operator to list models and stored in the agent Secret. Keep the UI cluster-internal.
- 0.0.6 is not published by this change. The cluster stays on the operator image it already runs until a release is cut and `gateway-helm` is bumped.
- Roll back by pinning the previous chart version.

## Follow-ups

- Publish 0.0.6 and bump the gateway subchart.
