# Security model

The inbound Codex bearer is request-scoped OpenAI material. It is not a general
gateway credential and is never written by this project.

The pre-auth hook resolves the model before key selection. Only a catalog model
owned by the exact `openai` provider and configured with
`request_passthrough` may retain `Authorization` and receive
`x-bf-direct-key: true`.

Hosted-tool fallback rewrites the request model before this credential
decision. Its target must be a cataloged native Responses model using
`request_passthrough`, so fallback traffic is subject to the same allowlist and
forwarded-token checks.

On every Bifrost-owned route the plugin removes, using case-insensitive
matching:

- `Authorization`;
- `x-api-key`;
- `x-goog-api-key`;
- caller-supplied `x-bf-direct-key`.

Unknown, missing, or ambiguous models fail closed. Bifrost must be configured
with `allow_direct_keys: true`, no stored OpenAI key, content logging disabled,
and provider credentials referenced through runtime environment variables.

Provider credentials live only in `providers.env` next to `config.json`
(`%APPDATA%\bifrost-model-router` on Windows, `~/.config/bifrost-model-router`
on Linux). That file must be a regular, non-symlink file inaccessible to group
and other users. The server reads it into the process environment and copies
no provider secret into Codex configuration.

The native server binds to loopback. Windows listens on `127.0.0.1:80`. Linux
listens on `127.0.0.1:8080` unless the setup script is given another address.

The Codex profile editor rejects symlinks and special files, writes through a
same-directory atomic rename, preserves restrictive modes, and creates a
0600 backup before modifying an existing file.

CI uses only canary keys. Pull-request tests require no live credentials. Live
provider keys must not be made available to workflows triggered by forks.
