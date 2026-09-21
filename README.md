# Bifrost Model Router

Use OpenAI and Bifrost-managed models from one Codex model provider. Native
Responses models pass through unchanged; Chat Completions-only models are
polyfilled by Bifrost. Codex's OpenAI credential is forwarded only to declared
OpenAI models and is stripped from every other provider route.

## Set up with Codex

Open this repository in Codex and ask it to set up the router. You do not need
to know the configuration format or provide a complete specification up front.
For example:

> Set this up for me.

Codex reads [AGENTS.md](AGENTS.md) and gathers the requirements with you. It
asks which provider plans or API accounts you want in Codex, researches their
current endpoints and model availability, explains any compatibility limits,
and configures only models available to those plans. You can name one or many
providers, including OpenRouter, MiniMax, Z.AI, Voke, Fireworks, or another
Bifrost-supported or OpenAI-compatible service.

Codex handles Docker, router configuration, catalog filtering, validation,
backups, and Codex settings. The only required secret-handling step is filling
the credential placeholders it creates in the mode-`0600` file
`~/.config/bifrost-model-router/providers.env`; credentials are never pasted
into chat or stored in the repository.

See the **[Codex setup guide](docs/codex-setup.md)** for prerequisites,
the agent-managed flow, verification, troubleshooting, and cleanup.

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
