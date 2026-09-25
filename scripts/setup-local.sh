#!/usr/bin/env bash
set -euo pipefail

core_container="bifrost-model-router-core"
gateway_container="bifrost-model-router-gateway"
docker_network="bifrost-model-router"
state_volume="bifrost-model-router-data"
image_name="${BIFROST_ROUTER_IMAGE:-ghcr.io/applyinnovations/bifrost-model-router:main}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "${script_dir}/.." && pwd)"
config_source="${repo_dir}/config/quickstart.json"
provider_env=""
default_model="gpt-5.6-sol"
reasoning_effort="medium"
hosted_tool_policy=""
accept_plaintext_key=false
accept_new_threads=false
replace_existing=false
state_dir="${XDG_STATE_HOME:-${HOME}/.local/state}/bifrost-model-router"
runtime_config="${state_dir}/config.json"
codex_dir="${CODEX_HOME:-${HOME}/.codex}"
codex_config="${codex_dir}/config.toml"

usage() {
	cat <<'EOF'
usage: setup-local.sh [options]

Options:
  --config PATH                  Bifrost config generated for the user's plans
  --env-file PATH                Mode-0600 provider credential environment file
  --model MODEL                  Default Codex model
  --reasoning-effort EFFORT      minimal, low, medium, high, or xhigh
  --image IMAGE                  OCI image override
  --hosted-tool-policy POLICY    fallback, strip, reject, or bridge for polyfilled models
  --accept-plaintext-key         Accept the local virtual-key notice
  --accept-new-threads-only      Accept that defaults affect new threads only
  --replace                      Replace existing quickstart containers
  -h, --help                     Show this help
EOF
}

parse_args() {
	while (($# > 0)); do
		case "$1" in
		--config | --env-file | --model | --reasoning-effort | --image | --hosted-tool-policy)
			if (($# < 2)); then
				printf 'error: %s requires a value\n' "$1" >&2
				exit 2
			fi
			case "$1" in
			--config) config_source="$2" ;;
			--env-file) provider_env="$2" ;;
			--model) default_model="$2" ;;
			--reasoning-effort) reasoning_effort="$2" ;;
			--image) image_name="$2" ;;
			--hosted-tool-policy)
				case "$2" in
				fallback | strip | reject | bridge) hosted_tool_policy="$2" ;;
				*)
					printf 'error: --hosted-tool-policy must be fallback, strip, reject, or bridge\n' >&2
					exit 2
					;;
				esac
				;;
			esac
			shift 2
			;;
		--accept-plaintext-key)
			accept_plaintext_key=true
			shift
			;;
		--accept-new-threads-only)
			accept_new_threads=true
			shift
			;;
		--replace)
			replace_existing=true
			shift
			;;
		-h | --help)
			usage
			exit 0
			;;
		*)
			printf 'error: unknown option: %s\n' "$1" >&2
			usage >&2
			exit 2
			;;
		esac
	done
}

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'error: required command not found: %s\n' "$1" >&2
		exit 1
	fi
}

file_mode() {
	case "$(uname -s)" in
	Darwin) stat -f '%Lp' "$1" ;;
	*) stat -c '%a' "$1" ;;
	esac
}

acknowledge() {
	local prompt="$1"
	local expected="$2"
	local answer

	printf '\n%s\n' "$prompt"
	printf 'Type %s to continue: ' "$expected"
	IFS= read -r answer
	if [[ "$answer" != "$expected" ]]; then
		printf 'Setup cancelled; no changes were made.\n' >&2
		exit 1
	fi
}

container_exists() {
	docker container inspect "$1" >/dev/null 2>&1
}

prepare_inputs() {
	local permissions required_name
	local -a required_names=()

	if [[ -L "$config_source" || ! -f "$config_source" ]]; then
		printf 'error: router config must be a regular, non-symlink file: %s\n' "$config_source" >&2
		exit 1
	fi
	jq empty "$config_source"
	if ! jq -e '[.providers[]?.keys[]?.value | select(type == "string" and (startswith("env.") | not))] | length == 0' "$config_source" >/dev/null; then
		printf 'error: provider credentials in the config must use env.NAME references\n' >&2
		exit 1
	fi
	if [[ ! "$default_model" =~ ^[A-Za-z0-9._:/-]+$ ]]; then
		printf 'error: unsafe default model name: %s\n' "$default_model" >&2
		exit 1
	fi
	case "$reasoning_effort" in
	minimal | low | medium | high | xhigh) ;;
	*)
		printf 'error: invalid reasoning effort: %s\n' "$reasoning_effort" >&2
		exit 1
		;;
	esac

	while IFS= read -r required_name; do
		required_names+=("$required_name")
	done < <(
		jq -r '.. | strings | select(startswith("env.")) | ltrimstr("env.")' "$config_source" |
			sort -u | grep -v '^BIFROST_QUICKSTART_VK$' || true
	)
	if [[ -n "$provider_env" ]]; then
		if [[ -L "$provider_env" || ! -f "$provider_env" ]]; then
			printf 'error: provider env file must be a regular, non-symlink file: %s\n' "$provider_env" >&2
			exit 1
		fi
		permissions="$(file_mode "$provider_env")"
		if (((8#$permissions & 077) != 0)); then
			printf 'error: provider env file must not be accessible by group or others (mode is %s)\n' "$permissions" >&2
			exit 1
		fi
	fi
	if ((${#required_names[@]} > 0)); then
		if [[ -z "$provider_env" ]]; then
			printf 'error: --env-file is required for provider credentials: %s\n' "${required_names[*]}" >&2
			exit 1
		fi
		for required_name in "${required_names[@]}"; do
			if [[ ! "$required_name" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || ! grep -Eq "^${required_name}=.+$" "$provider_env"; then
				printf 'error: provider env file has no non-empty %s entry\n' "$required_name" >&2
				exit 1
			fi
		done
	fi
}

stage_runtime_config() {
	mkdir -p "$state_dir"
	chmod 0700 "$state_dir"
	cp -- "$config_source" "$runtime_config"
	chmod 0644 "$runtime_config"
}

wait_for_health() {
	local attempts=1200
	local attempt

	for ((attempt = 1; attempt <= attempts; attempt++)); do
		if curl --fail --silent http://127.0.0.1/health >/dev/null; then
			return 0
		fi
		if ! docker container inspect --format '{{.State.Running}}' "$core_container" 2>/dev/null | grep -qx true; then
			printf 'error: Bifrost core container stopped during startup\n' >&2
			docker logs "$core_container" >&2 || true
			return 1
		fi
		if ! docker container inspect --format '{{.State.Running}}' "$gateway_container" 2>/dev/null | grep -qx true; then
			printf 'error: gateway container stopped during startup\n' >&2
			docker logs "$gateway_container" >&2 || true
			return 1
		fi
		sleep 0.25
	done

	printf 'error: router did not become healthy within five minutes\n' >&2
	docker logs "$core_container" >&2 || true
	docker logs "$gateway_container" >&2 || true
	return 1
}

install_codex_config() {
	local virtual_key="$1"
	local timestamp backup="" cleaned output

	mkdir -p "$codex_dir"
	if [[ -L "$codex_config" ]]; then
		printf 'error: refusing to replace symlink: %s\n' "$codex_config" >&2
		return 1
	fi
	if [[ -e "$codex_config" && ! -f "$codex_config" ]]; then
		printf 'error: Codex config is not a regular file: %s\n' "$codex_config" >&2
		return 1
	fi

	timestamp="$(date -u +%Y%m%dT%H%M%SZ).$$"
	if [[ -f "$codex_config" ]]; then
		backup="${codex_config}.bak.${timestamp}"
		cp -p -- "$codex_config" "$backup"
		chmod 0600 "$backup"
	fi

	cleaned="$(mktemp "${codex_dir}/.config.cleaned.XXXXXX")"
	output="$(mktemp "${codex_dir}/.config.toml.XXXXXX")"
	trap 'rm -f -- "${cleaned:-}" "${output:-}"' RETURN

	if [[ -f "$codex_config" ]]; then
		awk '
      BEGIN { skip = 0; skip_provider = 0; root = 1 }
      /^# BEGIN bifrost-model-router quickstart / { skip = 1; next }
      /^# END bifrost-model-router quickstart / { skip = 0; next }
      /^# BEGIN bifrost-model-router \(managed\)$/ { skip = 1; next }
      /^# END bifrost-model-router \(managed\)$/ { skip = 0; next }
      skip { next }
      skip_provider {
        if ($0 ~ /^\[/) { skip_provider = 0 } else { next }
      }
      /^\[model_providers\.bifrost-router(\.|\])/ { skip_provider = 1; next }
      root && /^\[/ { root = 0 }
      root && /^[[:space:]]*(model|model_provider|model_reasoning_effort)[[:space:]]*=/ { next }
      { print }
    ' "$codex_config" >"$cleaned"
	else
		: >"$cleaned"
	fi

	{
		printf '%s\n' '# BEGIN bifrost-model-router quickstart defaults (managed)'
		printf 'model = "%s"\n' "$default_model"
		printf '%s\n' 'model_provider = "bifrost-router"'
		printf 'model_reasoning_effort = "%s"\n' "$reasoning_effort"
		printf '%s\n\n' '# END bifrost-model-router quickstart defaults (managed)'
		sed '/./,$!d' "$cleaned"
		if [[ -s "$cleaned" ]]; then
			printf '\n'
		fi
		printf '%s\n' '# BEGIN bifrost-model-router quickstart provider (managed)'
		printf '%s\n' '[model_providers.bifrost-router]'
		printf '%s\n' 'name = "Local Bifrost Router"'
		printf '%s\n' 'base_url = "http://127.0.0.1/v1"'
		printf '%s\n' 'wire_api = "responses"'
		printf '%s\n' 'requires_openai_auth = true'
		printf 'http_headers = { "x-bf-vk" = "%s" }\n' "$virtual_key"
		printf '%s\n' '# END bifrost-model-router quickstart provider (managed)'
	} >"$output"

	chmod 0600 "$output"
	mv -f -- "$output" "$codex_config"
	rm -f -- "$cleaned"
	trap - RETURN

	printf 'Codex config: %s\n' "$codex_config"
	if [[ -n "$backup" ]]; then
		printf 'Backup: %s\n' "$backup"
	else
		printf 'Backup: none (the config did not previously exist)\n'
	fi
}

main() {
	local command virtual_key
	local -a provider_env_args=()

	parse_args "$@"
	for command in docker openssl curl awk sed grep jq stat cp sort; do
		require_command "$command"
	done

	if ! docker info >/dev/null 2>&1; then
		printf 'error: Docker is not available; start the daemon and check your access\n' >&2
		exit 1
	fi
	prepare_inputs

	if [[ "$accept_plaintext_key" != true ]]; then
		if [[ ! -t 0 ]]; then
			printf 'error: non-interactive setup requires --accept-plaintext-key\n' >&2
			exit 1
		fi
		acknowledge \
			'WARNING: this local-development setup stores the Bifrost virtual key as plaintext in ~/.codex/config.toml. Anyone who can read that file can use the key.' \
			'I-ACCEPT-PLAINTEXT-KEY'
	fi

	if [[ "$accept_new_threads" != true ]]; then
		if [[ ! -t 0 ]]; then
			printf 'error: non-interactive setup requires --accept-new-threads-only\n' >&2
			exit 1
		fi
		acknowledge \
			'WARNING: the provider and model defaults apply only to new Codex threads. Existing, resumed, and currently running threads will not change and may reject router models as unsupported ChatGPT models.' \
			'I-UNDERSTAND-NEW-THREADS-ONLY'
	fi
	stage_runtime_config

	if container_exists "$core_container" || container_exists "$gateway_container"; then
		if [[ "$replace_existing" != true ]]; then
			if [[ ! -t 0 ]]; then
				printf 'error: existing containers require --replace in non-interactive setup\n' >&2
				exit 1
			fi
			acknowledge \
				'Existing Bifrost Model Router quickstart containers will be replaced. The persistent Docker volume will be retained.' \
				'REPLACE-LOCAL-ROUTER'
		fi
		docker rm -f "$gateway_container" "$core_container" >/dev/null 2>&1 || true
	fi

	printf '\nPulling %s...\n' "$image_name"
	docker pull "$image_name"

	docker network inspect "$docker_network" >/dev/null 2>&1 || docker network create "$docker_network" >/dev/null
	docker volume inspect "$state_volume" >/dev/null 2>&1 || docker volume create "$state_volume" >/dev/null

	docker run --rm \
		--user 0:0 \
		--volume "${state_volume}:/var/lib/bifrost" \
		--entrypoint /bin/mkdir \
		"$image_name" -p /var/lib/bifrost/data
	docker run --rm \
		--user 0:0 \
		--volume "${state_volume}:/var/lib/bifrost" \
		--entrypoint /bin/chown \
		"$image_name" 65532:65532 /var/lib/bifrost/data

	virtual_key="sk-bf-$(openssl rand -hex 24)"
	if [[ -n "$provider_env" ]]; then
		provider_env_args=(--env-file "$provider_env")
	fi

	docker run --detach \
		--name "$core_container" \
		--network "$docker_network" \
		--workdir /var/lib/bifrost/data \
		--env BIFROST_APP_DIR=/var/lib/bifrost/data \
		--env BIFROST_HOST=0.0.0.0 \
		--env BIFROST_QUICKSTART_VK="$virtual_key" \
		"${provider_env_args[@]}" \
		--volume "${state_volume}:/var/lib/bifrost" \
		--volume "${runtime_config}:/etc/bifrost/config.json:ro" \
		"$image_name" >/dev/null

	docker run --detach \
		--name "$gateway_container" \
		--network "$docker_network" \
		--publish 127.0.0.1:80:8082 \
		--env GATEWAY_ADDR=0.0.0.0:8082 \
		--env "BIFROST_UPSTREAM_URL=http://${core_container}:8080" \
		${hosted_tool_policy:+--env "HOSTED_TOOL_POLICY=${hosted_tool_policy}"} \
		--volume "${runtime_config}:/etc/bifrost/config.json:ro" \
		--entrypoint /bin/gateway \
		"$image_name" >/dev/null

	printf 'Waiting for the local router to become healthy...\n'
	wait_for_health
	install_codex_config "$virtual_key"

	printf '\nSetup complete. Start a new Codex thread to use %s.\n' "$default_model"
	printf 'Do not resume an existing thread for router models; existing threads keep their original provider.\n'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	main "$@"
fi
