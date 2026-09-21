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

`upstream_model` controls the provider model sent after resolution and defaults
to the portion of the canonical slug after `provider/`. Optional
`context_variants` publish explicit catalog entries such as `-256k`, `-872k`,
and `-1m` without changing the unsuffixed model. Each variant must be a positive
multiple of 1,000 and no larger than the model's verified
`codex.max_context_window`. The effective percentage inherits from the base
profile when omitted:

```yaml
models:
  openai/gpt-5.6-sol:
    aliases: [gpt-5.6-sol]
    upstream_model: gpt-5.6-sol
    codex:
      context_window: 272000
      max_context_window: 872000
      effective_context_window_percent: 95
    context_variants:
      - context_window: 872000
```

The generated `gpt-5.6-sol-872k` entry routes to upstream
`gpt-5.6-sol`. Unknown suffixes are rejected. Unsuffixed entries retain context
and capability fields returned by the upstream catalog; configured values only
fill fields the provider omitted.

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
It also maps the `x-bf-vk` header from `BIFROST_API_KEY`, so every gateway
request is authorized independently by a Bifrost-managed virtual key.

Set `hosted_tool_fallback_model` to a catalog model that uses native Responses
and `request_passthrough` credentials. When a Chat Completions-polyfilled model
is requested with an OpenAI-hosted/server-side tool, the transport rewrites the
whole request to that model before credential selection. The fallback therefore
uses the caller's forwarded OpenAI token; the router never stores an OpenAI key.

`namespace` is not considered hosted. Bifrost flattens namespace members into
ordinary function tools for providers without native namespace support and
restores namespaced calls in the returned Responses payload.
