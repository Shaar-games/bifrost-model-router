# Agent-managed onboarding

When a user asks to set up, configure, add, remove, update, or troubleshoot
Bifrost Model Router, own the requirements gathering and implementation. Do not
require the user to understand Bifrost JSON, Docker networking, adapters, model
catalogs, or Codex provider configuration.

## Conversation contract

- Treat vague requests such as “set it up” as the start of requirements
  gathering, not as missing information or a reason to stop.
- Ask focused questions until the provider and plan scope is clear. Prefer one
  consolidated question at a time over a long questionnaire.
- First establish which provider plans or API accounts the user wants available
  in Codex. Support any Bifrost-native or OpenAI-compatible provider without
  advertising, recommending, or implying affiliation with specific downstream
  companies.
- Always retain OpenAI through the user's Codex login. This is an invariant,
  not a requirements question.
- Inspect `~/.config/bifrost-model-router`, the managed Codex block, and the
  named Docker containers to detect first-time versus additive setup. Preserve
  existing providers, credentials, and model entries by default. Ask only if
  the user explicitly requests removal or discovered state is contradictory.
- Default new Codex threads to `gpt-5.6-sol` with `medium` reasoning. Do not ask
  the user to choose a default. Change it only when the user explicitly asks or
  authenticated discovery proves it unavailable; in that case select the
  closest available OpenAI coding model and explain the fallback.
- Resolve informal names and likely typos through research, then confirm the
  interpretation instead of rejecting the request.
- Explain material constraints and tradeoffs in plain language. Ask for user
  input when a plan is ambiguous, API access is uncertain, provider
  documentation conflicts with account discovery, or a change would remove
  existing access.
- Never claim every provider is automatically compatible. Bifrost-native and
  OpenAI-compatible Chat Completions providers should normally work. A provider
  with another protocol or unusual authentication may require an adapter; if
  so, explain the gap and offer to implement or configure it.

## Requirements to discover

For each requested provider, determine:

1. Provider name and the exact subscription, coding plan, or API product.
2. Whether that plan includes API access and which API endpoint applies.
3. Credential type and a clear environment variable name.
4. Models actually available to that account and plan—not the provider's full
   marketing catalog.
5. Wire protocol: Bifrost native, OpenAI-compatible Chat Completions, or native
   Responses.
6. Verified model capabilities, context windows, modalities, reasoning levels,
   and tool support.
7. Existing local router state that must be merged and preserved.

Use current official provider documentation as the primary source. Use an
authenticated model-list endpoint when it is account-aware. If discovery is
global rather than plan-aware, intersect it with plan documentation. When
availability remains uncertain, perform a minimal model request after warning
that it may consume a small amount of plan quota.

Never invent model IDs, context windows, plan entitlements, or capabilities.
For an OpenAI-compatible provider with unverified richer capabilities, use the
conservative `chat_polyfill` and text-only catalog contract.

## Credential handoff

Never ask the user to paste a provider secret into chat, commit it, place it in
the Bifrost JSON, or place it in `~/.codex/config.toml`.

Create these local files outside the repository:

```text
~/.config/bifrost-model-router/config.json
~/.config/bifrost-model-router/providers.env
```

Create `providers.env` with mode `0600` and one empty assignment for each
required credential, for example:

```dotenv
MANAGED_PROVIDER_API_KEY=
```

Tell the user exactly which file to open, ask them to fill the values locally,
and wait for them to say it is ready. This is the normal and minimal required
human secret-handling step. Afterward, verify only that required variables are
non-empty; never print their values. Preserve existing credential entries when
adding another provider.

Provider keys in `config.json` must use `env.NAME` references matching this env
file. The setup script rejects literal provider credentials and env files that
are accessible by group or other users.

## Configuration invariants

Generate one complete Bifrost JSON configuration. Use
`config/quickstart.json`, `config/bifrost.example.json`, and
`docs/configuration.md` as structural references.

Prefer authenticated, account-aware model discovery. When the provider's model
endpoint reflects the models available to the supplied credential:

1. set the router provider's `discover_models` to `true`;
2. allow the provider credential and local virtual key to use discovered model
   IDs rather than maintaining a static per-model list;
3. use explicit router model entries only for aliases, context variants, or
   capability overrides.

The router then passes newly discovered upstream models into Codex immediately;
users do not need to regenerate configuration when their provider adds a model.
Provider-level `codex_defaults` supply conservative metadata for new models,
while fields returned by the upstream catalog take precedence.

If the provider has no model endpoint, or its endpoint is a global catalog that
does not reflect account/plan availability, disable `discover_models` and use
an explicit verified model list. This is the fallback, not the preferred path.

Use canonical router slugs in the form `provider/model`. For explicit
overrides, set `upstream_model` to the provider's exact model ID and keep
aliases unambiguous. For custom
OpenAI-compatible Chat providers, configure Bifrost with
`base_provider_type: openai`, allow Chat Completions and streaming, disable
native Responses and model listing when unsupported, and select a router
`chat_polyfill` adapter. Only advertise capabilities verified for that model.

Keep the exact `openai` router provider reserved for Codex request passthrough.
Never forward the Codex OpenAI bearer to a managed provider.

## Apply the setup

Before applying, summarize the resolved scope: providers, plans, enabled
models, credential variable names, and any unverified capabilities. State that
OpenAI passthrough remains enabled and that the default is `gpt-5.6-sol` with
`medium` reasoning unless an automatic availability fallback was necessary.
Tell the user that the local Bifrost virtual key is stored in their mode-`0600`
Codex config and that provider/model defaults affect only new threads.

Then run the low-level executor non-interactively:

```sh
./scripts/setup-local.sh \
  --config "$HOME/.config/bifrost-model-router/config.json" \
  --env-file "$HOME/.config/bifrost-model-router/providers.env" \
  --accept-plaintext-key \
  --accept-new-threads-only \
  --replace
```

Omit `--env-file` only when the generated config has no managed-provider
credential references. Do not pass `--replace` unless the existing named
containers belong to this project; inspect them first. The setup script backs
up and preserves unrelated Codex configuration. Its defaults are
`gpt-5.6-sol` and `medium`; pass `--model` or `--reasoning-effort` only for an
explicit user override or verified availability fallback.

## Verification and handoff

1. Verify both containers are running and `http://127.0.0.1/health` succeeds.
2. Verify the hydrated model catalog contains exactly the enabled model set.
3. For managed providers, make a minimal request to the selected default model
   when the user has accepted the possible quota use.
4. Confirm provider credentials do not appear in generated config, command
   output, logs, or Codex configuration.
5. Report the configured providers, plans, models, default, config location,
   env-file location, backup path, and verification performed.
6. Tell the user to start a new Codex thread. Do not suggest resuming an old
   thread with the new provider.

If verification fails, diagnose and continue working. Ask the user only for
information or actions that cannot be safely discovered or performed locally.
