# Bifrost Model Router

Use OpenAI and Bifrost-managed models through one Codex model provider. Native
Responses requests pass through unchanged; Chat Completions-only models are
translated by Bifrost. Codex's OpenAI credential is forwarded only to declared
OpenAI routes and is stripped from every managed-provider route.

## Install

The program that serves Codex is one native binary. It embeds Bifrost and the
gateway. Install a published build; Go, Nix, and Docker are not required to
run it.

Windows (PowerShell, not as administrator):

```powershell
$script = Join-Path $env:TEMP "setup-binary.ps1"
Invoke-WebRequest -Uri "https://github.com/Shaar-games/bifrost-model-router/releases/latest/download/setup-binary.ps1" -OutFile $script
powershell -NoProfile -ExecutionPolicy Bypass -File $script
```

Linux:

```sh
curl -fsSL https://github.com/Shaar-games/bifrost-model-router/releases/latest/download/setup-binary.sh | bash
```

From a clone of this repository, the same scripts are `scripts/setup-binary.ps1`
and `scripts/setup-binary.sh`.

The script picks the binary for this machine, checks its SHA-256, and installs
it for the current user. It starts the router only when a config file already
exists:

- Windows: `%APPDATA%\bifrost-model-router\config.json`, listening on
  `127.0.0.1:80`. Codex keeps `base_url = "http://127.0.0.1/v1"`. A Startup
  shortcut launches it at login.
- Linux: `~/.config/bifrost-model-router/config.json`, listening on
  `127.0.0.1:8080` because port 80 is privileged. Set the Codex provider
  `base_url` to `http://127.0.0.1:8080/v1`. A systemd user service starts it
  at login. Pass `--address 127.0.0.1:80` when that port is available.

Provider credentials stay in `providers.env` next to that config. The scripts
do not create or print them. The published builds cover Windows and Linux,
amd64 and arm64. macOS binaries are not in the release. On a Mac, run
`mise run build:darwin`, then start `dist/darwin-arm64/bifrost-model-router-server`
or the Intel binary with `-addr 127.0.0.1:8080`.

## Set up with Codex

Open this repository in Codex and ask it to set up the router. You do not need
to know the configuration format or provide a complete specification up front:

> Set this up for me.

Codex reads [AGENTS.md](AGENTS.md) and gathers the requirements with you. It
asks which provider plans or API accounts you want in Codex, researches their
current endpoints and account-visible models, explains compatibility limits,
and asks whether each provider should show every discovered model or only a
model/family allowlist. It uses authenticated model discovery when it reflects
that account. You can name one or many Bifrost-native or OpenAI-compatible
providers; other protocols may need an adapter.

OpenAI through the existing Codex login is always retained. Codex detects and
merges an existing router setup automatically, and defaults new threads to
`gpt-5.6-sol` with `medium` reasoning unless you explicitly request otherwise.

Codex writes the router configuration, catalog policy, backups, and Codex
settings. Run the installed native binary from the section above; Nix and
Docker are not required to serve Codex.
The only required secret-handling step is filling the credential placeholders
it creates in the mode-`0600` file
`~/.config/bifrost-model-router/providers.env`; credentials are never pasted
into chat or stored in the repository.

After setup, fully quit and reopen Codex, then create a new task. Existing
tasks retain their original provider and session state.

See the **[Codex setup guide](docs/codex-setup.md)** for prerequisites,
the agent-managed flow, verification, troubleshooting, and cleanup.

## How it works

- `native` models use their provider's Responses API.
- `chat_polyfill` models use Bifrost's Responses-to-Chat translation.
- OpenAI requests use the caller's Codex authentication; other providers use
  credentials managed by Bifrost.
- Hosted tools on polyfilled models reroute the whole request to an OpenAI
  Responses model. The fallback defaults to `openai/gpt-6-luna` when available
  through the configured OpenAI passthrough; it can be overridden.
- The Codex Images API generation path uses that native OpenAI Responses model
  and its hosted `image_generation` tool. The gateway adapts the result to the
  Images API response shape, so Codex login auth works without an API key.
- Account-aware provider catalogs are discovered dynamically. New upstream
  models flow through without editing a static router model list. An optional
  virtual-key allowlist can deliberately limit what a user sees: `"*"` admits
  all current and future discovered models, exact IDs pin a fixed selection,
  and validated `regex:` entries admit matching model families over time.
- Except for explicit exception overrides, model labels prefer OpenRouter
  editorial names, then the upstream display name, then a readable form of the
  model ID. Publisher prefixes are removed; managed routes append their
  configured hosting source, such as `Model (Provider)`, while direct OpenAI
  models have no redundant suffix. Overrides are not used as a model list.
- Context windows come from the configured provider when available. If its
  catalog omits them, the router dynamically fills an exact or unambiguous
  OpenRouter catalog match; unmatched models retain a conservative fallback.
  Per-model context limits do not need to be hardcoded in application config.
- Unknown models and unsupported features fail closed.

See [architecture](docs/architecture.md),
[configuration](docs/configuration.md),
[compatibility](docs/compatibility.md),
[security](docs/security.md), and [operations](docs/operations.md).

## Development

The host and plugin are built together with Go 1.27. This repository
officially builds against the Bifrost fork
[Shaar-games/bifrost](https://github.com/Shaar-games/bifrost), not the
published `maximhq/bifrost` modules. `go.mod` replaces `core` and
`transports` with that fork so Codex MCP namespace tools survive the
Responses-to-Chat conversion. A build that drops those `replace` lines
drops MCP tools.

Development tasks live in `mise.toml`. Install [mise](https://mise.jdx.dev),
then from this repository:

```sh
mise install
mise run check
mise run build
```

`mise run check` formats nothing by itself: it runs `vet` and `go test ./...`.
`mise run build` writes this machine's server, gateway, and mock provider to
`dist/native/`. `mise run build:all` cross-compiles the native server for
Windows and Linux. macOS builds (`mise run build:darwin`) need a Mac with
Xcode, because cgo requires the Apple SDK. `mise tasks` lists the rest,
including `fmt`, `run`, `mock`, and the Windows `install` and `restart` tasks.

`nix develop` still provides Go and the Nix checks. `nix flake check -L` runs
the complete validation suite. Provider secrets are runtime inputs; never add
them to Nix expressions, tracked configuration, or the Codex provider profile.

## License and attribution

Licensed under the [Apache License 2.0](LICENSE). This project is built heavily
on [Bifrost](https://github.com/maximhq/bifrost), copyright H3 Labs Inc. and
licensed under Apache 2.0. The supported core is the
[Shaar-games/bifrost](https://github.com/Shaar-games/bifrost) fork of that
project. See [NOTICE](NOTICE) for attribution details.
