# Configuration

The router configuration is versioned and validated strictly. Unknown fields,
unknown adapters, conflicting aliases, invalid reasoning defaults, and unsafe
credential modes stop plugin initialization. The schema is
`config/router.schema.json`; `config/router.example.yaml` shows every major
concept.

Provider fields:

- `credential_mode`: `request_passthrough` only for the exact `openai`
  provider, otherwise `bifrost`.
- `responses_mode`: `native`, `chat_polyfill`, or `unsupported`.
- `adapter`: `native`, `openai-chat`, `single-system-message`, or
  `strict-text-only`.

Models use canonical `provider/model` slugs and may have unambiguous aliases.
Model settings override provider Responses mode and adapter. Codex metadata is
conservatively defaulted; a polyfilled model may advertise only text input and
cannot claim hosted search or native image-detail support.

Validate and print the effective defaulted configuration with:

```sh
nix run .#config-check -- config/router.example.yaml
```

The plugin configuration is nested inside Bifrost's `plugins[].config`, as in
`config/bifrost.example.json`. Bifrost provider keys should use `env.NAME`.
Never put key literals in a tracked config or Nix expression.

Codex provider configuration is user-level. Generate it with `router profile
render` or install it reversibly with `router profile install`. The generated
profile uses the Responses wire API and Codex's native OpenAI authentication.
