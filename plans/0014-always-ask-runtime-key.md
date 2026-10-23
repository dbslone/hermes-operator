# 0014 – Always ask for the provider's runtime key

- **Status:** done
- **Date:** 2026-10-23
- **PR:** unreleased
- **Related plans:** [0012](0012-inline-provider-key.md), [0013](0013-anthropic-models-from-openrouter.md)

## Why

Plan 0013 hid the API key line for providers whose model list needs no key (OpenRouter, and Anthropic listed through OpenRouter). The only hint left was a note to add `OPENROUTER_API_KEY` under Advanced, which was easy to miss. Agents were created with the right `spec.model` and `spec.soul`, but with no `<name>-env` Secret and no `envFrom`. So Hermes had no credentials. Running `hermes model` in the pod to fix it then fell back to Anthropic OAuth, which needs the `claude` CLI and is not available in the image. To the user it looked as if the provider and model had not been saved.

This reverses the "API key line stays hidden" part of [0013](0013-anthropic-models-from-openrouter.md).

## What changes

- The API key line is shown for every known provider that has a `keyEnv`. `needsKey` now only controls whether listing models waits for a key.
- For a provider whose listing needs no key, typing a key does not reload the model list.
- Submitting without the provider's key, in either the key line or Advanced, shows a warning and does not create the agent. A second click creates it anyway, for operators that share keys through `defaults.envFromSecret`.
- The model note no longer says "Save … under Advanced". The OpenRouter and Anthropic key steps now point at the key line.

## Design and alternatives

Making the key required would break installs that share a key through the operator's defaults secret. The UI cannot see that secret. A warning on the first click keeps that setup working and still catches the common mistake. Passing a `hasSharedKey` flag from the server was rejected because a shared secret may hold any key, not only this provider's.

## How it is tested

| Behaviour | Layer | Test |
| --- | --- | --- |
| No-key-listing provider shows the key line, warns on create without a key, creates on second click | ui | `a provider whose listing needs no key still asks for the runtime key before create` |
| Key under Advanced counts as set | ui | `a runtime key under Advanced counts as set` |
| Anthropic through OpenRouter shows the key line, does not reload on typing, saves `OPENROUTER_API_KEY` | ui | `Anthropic listed from OpenRouter submits the OpenRouter provider` |

- Coverage after this change: unchanged (UI and YAML text only)
- Regression test added for any bug fixed: yes

## Risks, rollout, rollback

UI only. No CRD or API change. Agents created earlier without a key need one added by hand: create a Secret with `OPENROUTER_API_KEY` and add it to `spec.envFrom`.

## Follow-ups

- Let an existing agent's key, model and soul be edited from the UI. Today the only option is `kubectl edit`.
