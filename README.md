# Bifrost Model Router

A provider-agnostic Bifrost plugin that presents a Codex-compatible model
catalog, selects native or polyfilled Responses behavior per model, passes
Codex's OpenAI credential through only for OpenAI models, and uses
Bifrost-managed credentials everywhere else.

The router uses a capability-driven plugin: native Responses requests pass
through, while declared Chat-only providers are polyfilled by Bifrost. Codex's
OpenAI bearer is allowed only for configured `openai/*` models; it is stripped
before every Bifrost-credential route.

## Development

```sh
nix develop
just test
just check-config
```

Build the plugin and configuration checker with:

```sh
nix build .#default
```

The default package also contains the pinned Bifrost HTTP host. The host and
plugin use Go 1.27, Bifrost core v1.9.0, and the same Nix toolchain. CI starts
the packaged host with the packaged plugin to catch Go plugin ABI drift.

Individual outputs are available as `.#bifrost`, `.#plugin`, `.#router`, and
`.#config-check`.

## Configure Codex

The installer adds only a marked provider block to the user-level Codex
configuration, preserves all other text and comments, and writes a timestamped
backup before changes:

```sh
nix run .#router -- profile install --base-url http://127.0.0.1:8080/v1
```

It deliberately sets `requires_openai_auth = true` and does not store a key.
Use `profile uninstall` to remove the managed block or `profile rollback
BACKUP` to restore an exact backup. Run `profile render` to inspect the TOML
without changing anything.

## Run locally

Copy and edit `config/bifrost.example.json`, replacing its plugin path with the
path printed by `nix build .#plugin --print-out-paths`. Then run:

```sh
CEREBRAS_API_KEY=... nix run .#bifrost -- -app-dir /path/to/app-dir -host 127.0.0.1 -port 8080
```

The app directory must contain `config.json`; provider keys stay in runtime
environment variables or systemd credentials. See the
[configuration](docs/configuration.md), [compatibility](docs/compatibility.md),
[security](docs/security.md), and [operations](docs/operations.md) guides.

Provider secrets are runtime inputs. Do not add them to Nix expressions,
tracked configuration, or the Codex provider profile.
