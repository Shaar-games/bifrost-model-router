# Codex setup

The router runs as the native server from a GitHub release. Install it with
the scripts in the [README](../README.md#install). Docker and Nix are not
required.

The server starts only when its config file already exists:

- Windows: `%APPDATA%\bifrost-model-router\config.json`
- Linux: `~/.config/bifrost-model-router/config.json`

Provider secrets go in `providers.env` in that same directory, mode `0600`,
as `env.NAME` references. Do not paste them into chat or into Codex config.

Windows listens on `127.0.0.1:80`, so the Codex provider uses
`base_url = "http://127.0.0.1/v1"`. Linux listens on `127.0.0.1:8080` by
default, so that provider uses `base_url = "http://127.0.0.1:8080/v1"`.

The setup script does not edit Codex config. After it finishes, run the
command it prints. The same commands are:

Windows:

```powershell
$env:BIFROST_ROUTER_ADDRESS='127.0.0.1:80'; $env:BIFROST_ROUTER_CONFIGURE_CODEX='1'; iex (irm "https://github.com/Shaar-games/bifrost-model-router/releases/latest/download/setup-binary.ps1")
```

Linux:

```sh
curl -fsSL https://github.com/Shaar-games/bifrost-model-router/releases/latest/download/setup-binary.sh | bash -s -- --configure-codex --address 127.0.0.1:8080
```

The command backs up `config.toml`, writes `model_provider = "bifrost-router"`,
and stores the virtual key from `providers.env` in that file without printing
it. New threads default to `gpt-5.6-sol` with `medium` reasoning. If
`bifrost-router` already points at that address, the file is left unchanged.
Fully quit and reopen Codex, then create a new task. Existing tasks keep their
original provider.
