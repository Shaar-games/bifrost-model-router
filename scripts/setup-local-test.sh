#!/usr/bin/env bash
# shellcheck disable=SC1091,SC2034,SC2154
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "${script_dir}/.." && pwd)"
test_root="$(mktemp -d)"
trap 'rm -rf -- "$test_root"' EXIT

export CODEX_HOME="$test_root/codex"
export XDG_STATE_HOME="$test_root/state"
source "$script_dir/setup-local.sh"
test "$default_model" = "gpt-5.6-sol"
test "$reasoning_effort" = "medium"

mkdir -p "$test_root/input"
jq '
  .providers.zai = {
    keys: [{name: "zai", value: "env.ZAI_API_KEY", models: ["glm-test"], weight: 1}]
  } |
  .governance.virtual_keys[0].provider_configs += [{
    provider: "zai", allowed_models: ["glm-test"], blacklisted_models: [], key_ids: ["*"]
  }] |
  .plugins[1].config.providers.zai = {
    credential_mode: "bifrost", responses_mode: "chat_polyfill", adapter: "openai-chat"
  } |
  .plugins[1].config.models["zai/glm-test"] = {
    aliases: ["glm-test"], upstream_model: "glm-test",
    codex: {context_window: 64000, input_modalities: ["text"]}
  }
' "$repo_dir/config/quickstart.json" >"$test_root/input/config.json"
printf '%s\n' 'ZAI_API_KEY=test-only' >"$test_root/input/providers.env"
chmod 0600 "$test_root/input/providers.env"

config_source="$test_root/input/config.json"
provider_env="$test_root/input/providers.env"
default_model="zai/glm-test"
reasoning_effort="medium"

prepare_inputs
stage_runtime_config
test -f "$runtime_config"
test "$(stat -c '%a' "$runtime_config")" = 644

install_codex_config sk-bf-test-only >/dev/null
grep -Fqx 'model = "zai/glm-test"' "$codex_config"
grep -Fqx 'model_reasoning_effort = "medium"' "$codex_config"
grep -Fqx 'http_headers = { "x-bf-vk" = "sk-bf-test-only" }' "$codex_config"
test "$(stat -c '%a' "$codex_config")" = 600

chmod 0644 "$provider_env"
if (prepare_inputs) >/dev/null 2>&1; then
	echo "expected an insecure provider env file to be rejected" >&2
	exit 1
fi

printf 'agent-managed setup executor tests passed\n'
