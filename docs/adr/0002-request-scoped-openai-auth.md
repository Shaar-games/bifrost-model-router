# ADR 0002: Route Codex OpenAI auth per request

Status: accepted

One Codex custom provider uses `wire_api = "responses"` and
`requires_openai_auth = true`. For declared OpenAI models, the plugin enables
Bifrost's direct-key path so Codex's existing authentication reaches OpenAI.
For all other providers it strips the bearer and lets Bifrost select a stored
provider credential. This preserves native Codex login behavior without asking
Bifrost to persist OpenAI credentials and prevents cross-provider leakage.
