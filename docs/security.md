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

The NixOS module binds to loopback by default, uses a dynamic user, a private
temporary directory, an explicit state directory, no capabilities, a strict
read-only system, and systemd `LoadCredential`. It reads each credential into
the named provider environment variable only in the service process.

The Codex profile editor rejects symlinks and special files, writes through a
same-directory atomic rename, preserves restrictive modes, and creates a
0600 backup before modifying an existing file.

CI uses only canary keys. Pull-request tests require no live credentials. Live
provider keys must not be made available to workflows triggered by forks.
