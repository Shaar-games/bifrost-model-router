# ADR 0001: Use an ABI-matched native Bifrost plugin

Status: accepted

The required pre-auth, post-transport, LLM, and streaming conversion hooks are
available to the pinned native Go plugin API. A separate facade would add
another streaming proxy and duplicate Bifrost's Responses mux. The project
therefore packages the host and plugin from one locked Go dependency graph and
tests real plugin loading. Independently built `.so` artifacts are unsupported
and are never committed.
