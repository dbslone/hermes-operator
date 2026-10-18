# 0010 – UI test runner

- **Status:** done
- **Date:** 2026-10-18
- **PR:** tests
- **Related plans:** [0009 Model field is a real dropdown](0009-model-select-dropdown.md) · [0002 PR review and test gates](0002-pr-review-and-test-gates.md)

## Why

`internal/server/ui.html` is embedded plain HTML with ~100 lines of JavaScript, and the Go unit tests cannot exercise it. Plan 0009 verified the dropdown behaviour with a throwaway script. Without a committed runner, UI regressions (hidden fields, wrong submitted model) would only be found by hand.

## What changes

- `test/ui/`: `package.json` (only dev dependency `jsdom`), a committed `package-lock.json`, and `ui.test.js` using Node's built-in `node:test` runner. No framework, bundler or build step; the application itself stays dependency-free.
- Each test loads the real `ui.html` in jsdom with a fake `fetch` and drives it (open dialog, pick provider, type keys, submit), then asserts on the DOM and the request bodies sent.
- `make ui-test` runs it in a `node:22` container (same "no local toolchain" rule as Go); `make check` includes it.
- CI: new `ui` job in `ci.yaml` (`setup-node`, `npm ci`, `npm test`).
- `CLAUDE.md`: UI changes need a case in `test/ui/ui.test.js`.

## How it is tested

| Behaviour | Layer | Test |
| --- | --- | --- |
| Provider list: default, providers, Other… | UI | `provider select lists default…` |
| Text box with no provider; select after choosing one | UI | `without a provider…`, `choosing a provider…` |
| Chosen/custom model and provider submitted | UI | `the selected model…`, `Other… reveals…`, `custom provider id…` |
| Key needed → text box + reason; key typed → list loads | UI | `a provider that needs a key…`, `the key typed…` |
| Selection kept on reload; empty list; providers down | UI | `reloading…`, `an empty model list…`, `providers failing…` |
| Agent cards render | UI | `agent cards render…` |

- Mutation check: breaking the select-reveal line in `ui.html` fails 4 of 12 tests.
- Coverage after this change: unchanged for Go (97.9%); JS coverage is not measured.
- Regression test added for any bug fixed: n/a

## Risks, rollout, rollback

- Adds a Node toolchain to CI and `make check` (needs Docker locally). jsdom is a dev-only dependency; nothing ships in the image.
- The page's 4-second polling `setInterval` would keep Node alive, so the suite closes every jsdom window at the end. Keep that when editing the harness.
- jsdom is not a real browser: layout, `<dialog>` modality and CSS are not tested (`showModal` is stubbed).

## Follow-ups

- A browser-level smoke test (Playwright) against a kind cluster in the e2e job.
- Measure JS coverage if the UI grows.
