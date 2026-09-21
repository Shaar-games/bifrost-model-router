#!/usr/bin/env bash
set -euo pipefail

core_container="bifrost-model-router-core"
gateway_container="bifrost-model-router-gateway"
docker_network="bifrost-model-router"
state_volume="bifrost-model-router-data"
image_name="${BIFROST_ROUTER_IMAGE:-ghcr.io/applyinnovations/bifrost-model-router:main}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repo_dir="$(cd -- "${script_dir}/.." && pwd)"
runtime_config="${repo_dir}/config/quickstart.json"
codex_dir="${CODEX_HOME:-${HOME}/.codex}"
codex_config="${codex_dir}/config.toml"

require_command() {
	if ! command -v "$1" >/dev/null 2>&1; then
		printf 'error: required command not found: %s\n' "$1" >&2
		exit 1
	fi
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

	timestamp="$(date -u +%Y%m%dT%H%M%S.%NZ)"
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
		printf '%s\n' 'model = "gpt-5.6-sol"'
		printf '%s\n' 'model_provider = "bifrost-router"'
		printf '%s\n' 'model_reasoning_effort = "medium"'
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

	for command in docker codex openssl curl awk sed grep; do
		require_command "$command"
	done

	if [[ ! -t 0 ]]; then
		printf 'error: setup requires an interactive terminal for acknowledgements\n' >&2
		exit 1
	fi

	if ! docker info >/dev/null 2>&1; then
		printf 'error: Docker is not available; start the daemon and check your access\n' >&2
		exit 1
	fi
	if ! codex login status >/dev/null 2>&1; then
		printf 'error: Codex is not signed in; run codex login, then rerun setup\n' >&2
		exit 1
	fi

	acknowledge \
		'WARNING: this local-development setup stores the Bifrost virtual key as plaintext in ~/.codex/config.toml. Anyone who can read that file can use the key.' \
		'I-ACCEPT-PLAINTEXT-KEY'

	acknowledge \
		'WARNING: the provider and model defaults apply only to new Codex threads. Existing, resumed, and currently running threads will not change and may reject router models as unsupported ChatGPT models.' \
		'I-UNDERSTAND-NEW-THREADS-ONLY'

	if container_exists "$core_container" || container_exists "$gateway_container"; then
		acknowledge \
			'Existing Bifrost Model Router quickstart containers will be replaced. The persistent Docker volume will be retained.' \
			'REPLACE-LOCAL-ROUTER'
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

	docker run --detach \
		--name "$core_container" \
		--network "$docker_network" \
		--workdir /var/lib/bifrost/data \
		--env BIFROST_APP_DIR=/var/lib/bifrost/data \
		--env BIFROST_HOST=0.0.0.0 \
		--env BIFROST_QUICKSTART_VK="$virtual_key" \
		--volume "${state_volume}:/var/lib/bifrost" \
		--volume "${runtime_config}:/etc/bifrost/config.json:ro" \
		"$image_name" >/dev/null

	docker run --detach \
		--name "$gateway_container" \
		--network "$docker_network" \
		--publish 127.0.0.1:80:8082 \
		--env GATEWAY_ADDR=0.0.0.0:8082 \
		--env "BIFROST_UPSTREAM_URL=http://${core_container}:8080" \
		--volume "${runtime_config}:/etc/bifrost/config.json:ro" \
		--entrypoint /bin/gateway \
		"$image_name" >/dev/null

	printf 'Waiting for the local router to become healthy...\n'
	wait_for_health
	install_codex_config "$virtual_key"

	printf '\nSetup complete. Start a new Codex thread to use gpt-5.6-sol.\n'
	printf 'Do not resume an existing thread for router models; existing threads keep their original provider.\n'
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
	main "$@"
fi
