# Bifrost Model Router

Use OpenAI and Bifrost-managed models from one Codex model provider. Native
Responses models pass through unchanged; Chat Completions-only models are
polyfilled by Bifrost. Codex's OpenAI credential is forwarded only to declared
OpenAI models and is stripped from every other provider route.

## Quick start

With Docker and the Codex CLI installed, run:

```sh
./scripts/setup-local.sh
```

The script pulls the published GHCR image, starts it on `http://127.0.0.1`, backs up
`~/.codex/config.toml`, and makes `gpt-5.6-sol` the default for new Codex
threads. It requires you to acknowledge that the local virtual key is stored
in plaintext and that existing threads keep their current provider and model.

See the **[Codex setup guide](docs/codex-setup.md)** for prerequisites,
the installed configuration, verification, troubleshooting, and cleanup.

## How it works

- `native` models use their provider's Responses API.
- `chat_polyfill` models use Bifrost's Responses-to-Chat translation.
- OpenAI requests use the caller's Codex authentication; other providers use
  credentials managed by Bifrost.
- Unknown models and unsupported features fail closed.

See [architecture](docs/architecture.md),
[configuration](docs/configuration.md),
[compatibility](docs/compatibility.md),
[security](docs/security.md), and [operations](docs/operations.md).

## Development

The host and plugin are built together with Go 1.27 and Bifrost core v1.9.0 to
preserve Go plugin ABI compatibility.

```sh
nix develop
just test
just check-config
nix build .#default
```

Run `nix flake check -L` for the complete validation suite. Provider secrets
are runtime inputs; never add them to Nix expressions, tracked configuration,
or the Codex provider profile.

## License and attribution

Licensed under the [Apache License 2.0](LICENSE). This project is built heavily
on [Bifrost](https://github.com/maximhq/bifrost), copyright H3 Labs Inc. and
licensed under Apache 2.0. See [NOTICE](NOTICE) for attribution details.
