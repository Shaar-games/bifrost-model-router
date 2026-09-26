# Codex setup

The router runs as the native server from a GitHub release. Install it with
the scripts in the [README](../README.md#install). Docker and Nix are not
required.

The server starts only when its config file already exists:

- Windows: `%APPDATA%\bifrost-model-router\config.json`
- Linux: `~/.config/bifrost-model-router/config.json`

Provider secrets go in `providers.env` in that same directory, mode `0600`,
as `env.NAME` references. Do not paste them into chat or into Codex config.

Windows listens on `127.0.0.1:80`, so the Codex provider uses:

```toml
base_url = "http://127.0.0.1/v1"
```

Linux listens on `127.0.0.1:8080` by default:

```toml
base_url = "http://127.0.0.1:8080/v1"
```

Install that provider block with `router profile install`. It keeps a backup
of the existing Codex config. Fully quit and reopen Codex, then create a new
task. Existing tasks keep their original provider.
