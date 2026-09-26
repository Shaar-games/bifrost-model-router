# Operations

## Deployment

Install the published native server. Windows and Linux builds are on the
GitHub release. The setup scripts are `scripts/setup-binary.ps1` and
`scripts/setup-binary.sh`.

The server reads:

- Windows: `%APPDATA%\bifrost-model-router\config.json` and `providers.env`
- Linux: `~/.config/bifrost-model-router/config.json` and `providers.env`

Windows listens on `127.0.0.1:80`. Linux listens on `127.0.0.1:8080`. Keep the
process on loopback. Put a TLS terminator in front of it before exposing it
to another machine.

## Health and verification

- `GET /health` verifies that the server is accepting requests.
- `GET /v1/models?client_version=operator-check` exercises Codex hydration.
- A native OpenAI request without Codex auth must return 401.
- A configured non-OpenAI request must work without passing the inbound OpenAI
  bearer to its upstream.

## Upgrade and rollback

Upgrade by installing a newer release binary with the same setup script. The
script replaces the binary and restarts the server. Config and provider
credentials stay in the config directory.

Codex profile changes are independently reversible with `router profile
uninstall` or `router profile rollback BACKUP`.

## Failure triage

- `unresolved_model`: add the canonical slug or a unique alias to router config.
- `polyfill_*` error: the request uses a feature excluded by the model adapter.
- Provider 401/403 on a non-OpenAI model: verify the Bifrost `env.NAME`
  reference in `providers.env`. Do not add Codex auth as a workaround.
- Catalog lacks a model: verify that account-aware discovery is enabled and the
  authenticated upstream lists it. For providers without model discovery,
  declare the model explicitly in router config.
