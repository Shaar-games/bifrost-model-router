# Architecture

Codex talks to one Bifrost endpoint using the Responses API. The project-owned
Go plugin supplies three policies from one immutable, validated configuration:

1. The pre-auth hook resolves the model. Only an exact `openai` provider with
   `request_passthrough` may activate Bifrost's direct-key path. Every other
   route loses inbound `Authorization`, provider-key, and direct-key headers.
2. The LLM hook selects `native`, `chat_polyfill`, or `unsupported` by declared
   model capability. Polyfill mode asks Bifrost core to translate Responses to
   Chat Completions and translate the result or SSE stream back.
3. The post-transport hook hydrates successful Codex model-list requests while
   preserving unknown upstream fields and adding configured undiscoverable
   models, including OpenAI models for which Bifrost stores no key.

The native plugin and host are built with Go 1.27 against Bifrost core v1.9.0.
The Bifrost transport source is pinned in `flake.lock`; the plugin-load check
fails if shared Go package identities drift.

No provider name is hard-coded for compatibility behavior. Provider names are
configuration keys; the sole deliberate exception is the security rule that
request credential passthrough is allowed only for the exact `openai` key.

See [ADR 0001](adr/0001-native-bifrost-plugin.md) for the plugin decision and
[ADR 0002](adr/0002-request-scoped-openai-auth.md) for the authentication flow.
