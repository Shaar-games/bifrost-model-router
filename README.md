# Bifrost Model Router

A provider-agnostic Bifrost plugin that presents a Codex-compatible model
catalog, selects native or polyfilled Responses behavior per model, passes
Codex's OpenAI credential through only for OpenAI models, and uses
Bifrost-managed credentials everywhere else.

The implementation is in progress. The accepted architecture and delivery
sequence are documented in [the implementation plan](docs/IMPLEMENTATION_PLAN.md).

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

Provider secrets are runtime inputs. Do not add them to Nix expressions,
tracked configuration, or the Codex provider profile.
