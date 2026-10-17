# 0009 – Model field is a real dropdown

- **Status:** done
- **Date:** 2026-10-16
- **PR:** release 0.0.4
- **Related plans:** [0008 Dynamic provider and model dropdowns](0008-dynamic-provider-and-model-dropdowns.md)

## Why

Plan 0008 made Model an `<input>` with a `<datalist>`. That only shows suggestions after focusing or typing, so users did not see a dropdown of the available models. This replaces that part of 0008 (the datalist decision); the rest of 0008 stands.

## What changes

- Once a provider's models load, Model becomes a `<select>`: `(Hermes default)`, every model, then `Other…`. `Other…` reveals the text box for any id not in the list.
- With no provider, or when the list cannot be loaded (no key, network error, empty list), only the text box shows and a note says why.
- Reloading the list (for example after pasting an API key) keeps the selected model if it is still in the list.
- Version 0.0.4 (Chart, MCP serverInfo, README).

## How it is tested

| Behaviour | Layer | Check |
| --- | --- | --- |
| Text box without a provider; select after choosing one | manual (jsdom script) | provider change → `modelSel` visible, `model` hidden |
| Options are default + models + Other… | manual (jsdom script) | option values compared |
| Chosen and custom ids are submitted | manual (jsdom script) | POST body `model.name` |
| No key → text box + note; key → select | manual (jsdom script) | env change reloads |
| Server endpoints unchanged | unit | `make cover` (97.9%) |

- The UI is plain embedded HTML; there is no JS test runner in the repo. The jsdom script was run once by hand and is not committed.
- Regression test added for any bug fixed: no (UI-only; see above)

## Risks, rollout, rollback

UI only. Roll back by pinning the previous chart version.

## Follow-ups

- Add a JS test runner (jsdom) to CI so UI behaviour is tested automatically.
- A type-to-filter box for very long lists (OpenRouter has ~460 models); native selects support type-to-jump.
