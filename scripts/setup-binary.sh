#!/usr/bin/env bash
# Installs the published Bifrost Model Router binary for the current user.
# Downloads the matching Linux or macOS asset, checks SHA-256, and starts it
# when the platform config file already exists. Does not create credentials.
set -euo pipefail

version="latest"
address="127.0.0.1:8080"
repository="Shaar-games/bifrost-model-router"

usage() {
	cat <<'EOF'
usage: setup-binary.sh [--version VERSION] [--address HOST:PORT]
       setup-binary.sh --configure-codex [--address HOST:PORT]

Downloads the native server for this machine from the GitHub release and
installs it under ${XDG_DATA_HOME:-$HOME/.local/share}/bifrost-model-router.
The default listen address is 127.0.0.1:8080. Point Codex base_url at
http://127.0.0.1:8080/v1, or pass --address 127.0.0.1:80 when that port is
available. macOS release binaries are not published; build them on a Mac.

--configure-codex writes the bifrost-router provider into the Codex user
config and keeps a backup. It reads the virtual key from providers.env and
does not print it.
EOF
}

configure_codex=0

while [[ $# -gt 0 ]]; do
	case "$1" in
	--version)
		version="${2:?--version needs a value}"
		shift 2
		;;
	--address)
		address="${2:?--address needs a value}"
		shift 2
		;;
	--configure-codex)
		configure_codex=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "unknown option: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ ! "$address" =~ ^[A-Za-z0-9._-]+:[0-9]+$ ]]; then
	echo "invalid address '$address' (expected host:port)" >&2
	exit 1
fi

case "$(uname -s)" in
Linux) goos="linux" ;;
Darwin) goos="darwin" ;;
*)
	echo "unsupported operating system: $(uname -s)" >&2
	exit 1
	;;
esac

case "$(uname -m)" in
x86_64 | amd64) goarch="amd64" ;;
aarch64 | arm64) goarch="arm64" ;;
*)
	echo "unsupported architecture: $(uname -m)" >&2
	exit 1
	;;
esac

codex_base_url() {
	local host="${address%%:*}" port="${address##*:}"
	if [[ "$port" == "80" ]]; then
		printf 'http://%s/v1' "$host"
	else
		printf 'http://%s:%s/v1' "$host" "$port"
	fi
}

read_env_assignment() {
	local file="$1" want="$2" line key value
	[[ -f "$file" ]] || return 1
	while IFS= read -r line || [[ -n "$line" ]]; do
		line="${line%$'\r'}"
		[[ "$line" =~ ^[[:space:]]*# ]] && continue
		[[ "$line" =~ ^[[:space:]]*([A-Za-z_][A-Za-z0-9_]*)[[:space:]]*=[[:space:]]*(.*)$ ]] || continue
		key="${BASH_REMATCH[1]}"
		[[ "$key" == "$want" ]] || continue
		value="${BASH_REMATCH[2]}"
		value="${value#"${value%%[![:space:]]*}"}"
		value="${value%"${value##*[![:space:]]}"}"
		case "$value" in
		\"*\") value="${value#\"}" value="${value%\"}" ;;
		\'*\') value="${value#\'}" value="${value%\'}" ;;
		esac
		[[ -n "$value" ]] || return 1
		printf '%s' "$value"
		return 0
	done <"$file"
	return 1
}

resolve_virtual_key() {
	local config_file="$1" env_file="$2" value name
	if value="$(read_env_assignment "$env_file" BIFROST_QUICKSTART_VK)"; then
		printf '%s' "$value"
		return 0
	fi
	if command -v python3 >/dev/null 2>&1 && [[ -f "$config_file" ]]; then
		if name="$(python3 - "$config_file" <<'PY'
import json, sys
with open(sys.argv[1], encoding="utf-8") as handle:
    data = json.load(handle)
for entry in (data.get("governance") or {}).get("virtual_keys") or []:
    value = str((entry or {}).get("value") or "")
    if value.startswith("env.") and value[4:].replace("_", "").isalnum():
        sys.stdout.write(value[4:])
        raise SystemExit(0)
    if value.startswith("sk-bf-") and "\n" not in value and '"' not in value and "\\" not in value:
        sys.stdout.write("literal:" + value)
        raise SystemExit(0)
raise SystemExit(1)
PY
)"; then
			case "$name" in
			literal:*)
				printf '%s' "${name#literal:}"
				return 0
				;;
			*)
				if value="$(read_env_assignment "$env_file" "$name")"; then
					printf '%s' "$value"
					return 0
				fi
				;;
			esac
		fi
	fi
	if [[ -f "$config_file" ]]; then
		while IFS= read -r name; do
			[[ -n "$name" ]] || continue
			if value="$(read_env_assignment "$env_file" "$name")"; then
				printf '%s' "$value"
				return 0
			fi
		done < <(grep -oE '"value"[[:space:]]*:[[:space:]]*"env\.[A-Za-z_][A-Za-z0-9_]*VK"' "$config_file" | sed -E 's/.*env\.([A-Za-z_][A-Za-z0-9_]*)".*/\1/' || true)
		value="$(grep -oE '"value"[[:space:]]*:[[:space:]]*"sk-bf-[^"]*"' "$config_file" | head -n 1 | sed -E 's/.*"([^"]*)"[[:space:]]*$/\1/' || true)"
		if [[ -n "$value" ]]; then
			printf '%s' "$value"
			return 0
		fi
	fi
	return 1
}

codex_is_ready() {
	local file="$1" base_url="$2"
	[[ -f "$file" ]] || return 1
	grep -Eq '^model_provider = "bifrost-router"$' "$file" || return 1
	grep -Fq "base_url = \"${base_url}\"" "$file" || return 1
	grep -Fq "x-bf-vk" "$file" || return 1
}

write_codex_quickstart() {
	local existing="$1" base_url="$2"
	awk -v base_url="$base_url" '
		function trim(value, copy) {
			copy = value
			sub(/^[ \t\r]+/, "", copy)
			sub(/[ \t\r]+$/, "", copy)
			return copy
		}
		function blank(value, copy) {
			copy = value
			sub(/^[ \t\r]+/, "", copy)
			sub(/[ \t\r]+$/, "", copy)
			return copy == ""
		}
		BEGIN {
			key = ENVIRON["BIFROST_CODEX_VK"]
			begin_defaults = "# BEGIN bifrost-model-router quickstart defaults (managed)"
			end_defaults = "# END bifrost-model-router quickstart defaults (managed)"
			begin_provider = "# BEGIN bifrost-model-router quickstart provider (managed)"
			end_provider = "# END bifrost-model-router quickstart provider (managed)"
			begin_managed = "# BEGIN bifrost-model-router (managed)"
			end_managed = "# END bifrost-model-router (managed)"
		}
		{
			line = $0
			sub(/\r$/, "", line)
			trimmed = trim(line)
			if (!skip && (trimmed == begin_defaults || trimmed == begin_provider || trimmed == begin_managed)) {
				skip = trimmed
				next
			}
			if (skip == begin_defaults && trimmed == end_defaults) { skip = ""; next }
			if (skip == begin_provider && trimmed == end_provider) { skip = ""; next }
			if (skip == begin_managed && trimmed == end_managed) { skip = ""; next }
			if (skip) next
			if (!in_table && (trimmed == "[model_providers.bifrost-router]" || index(trimmed, "[model_providers.bifrost-router.") == 1)) {
				in_table = 1
				next
			}
			if (in_table && substr(trimmed, 1, 1) == "[") in_table = 0
			if (in_table) next
			if (!seen_table && substr(trimmed, 1, 1) == "[") seen_table = 1
			if (!seen_table && (index(trimmed, "model =") == 1 || index(trimmed, "model_provider =") == 1 || index(trimmed, "model_reasoning_effort =") == 1)) next
			kept[++count] = line
		}
		END {
			if (skip) {
				print "malformed Codex block" > "/dev/stderr"
				exit 2
			}
			print begin_defaults
			print "model = \"gpt-5.6-sol\""
			print "model_provider = \"bifrost-router\""
			print "model_reasoning_effort = \"medium\""
			print end_defaults
			print ""
			start = 1
			finish = count
			while (start <= finish && blank(kept[start])) start++
			while (finish >= start && blank(kept[finish])) finish--
			if (start <= finish) {
				for (i = start; i <= finish; i++) print kept[i]
				print ""
			}
			print begin_provider
			print "[model_providers.bifrost-router]"
			print "name = \"Local Bifrost Router\""
			print "base_url = \"" base_url "\""
			print "wire_api = \"responses\""
			print "requires_openai_auth = true"
			print "http_headers = { \"x-bf-vk\" = \"" key "\" }"
			print end_provider
		}
	' "$existing"
}

install_codex_provider() {
	local base_url config_file env_file codex_home codex_config key backup temporary
	base_url="$(codex_base_url)"
	if [[ "$goos" == "darwin" ]]; then
		config_file="${HOME}/Library/Application Support/bifrost-model-router/config.json"
	else
		config_file="${XDG_CONFIG_HOME:-${HOME}/.config}/bifrost-model-router/config.json"
	fi
	env_file="$(dirname "$config_file")/providers.env"
	codex_home="${CODEX_HOME:-${HOME}/.codex}"
	codex_config="${codex_home}/config.toml"
	if [[ -L "$codex_config" ]] || { [[ -e "$codex_config" ]] && [[ ! -f "$codex_config" ]]; }; then
		echo "Refusing to edit ${codex_config}. Codex config must be a regular file." >&2
		exit 1
	fi
	if codex_is_ready "$codex_config" "$base_url"; then
		echo "Codex already uses bifrost-router at ${base_url}"
		echo "Config: ${codex_config}"
		echo "Fully quit Codex and open a new task."
		return 0
	fi
	if ! key="$(resolve_virtual_key "$config_file" "$env_file")"; then
		echo "No virtual key found. Set BIFROST_QUICKSTART_VK in ${env_file}, then run the configure command again." >&2
		exit 1
	fi
	case "$key" in
	*\"* | *\\* | *\$* | *$'\n'*)
		echo "Virtual key contains characters that cannot be stored in the Codex config." >&2
		exit 1
		;;
	esac
	mkdir -p "$codex_home"
	chmod 700 "$codex_home" 2>/dev/null || true
	temporary="$(mktemp "${codex_home}/.bifrost-router-config.XXXXXX")"
	if [[ -f "$codex_config" ]]; then
		backup="${codex_config}.bak.$(date -u +%Y%m%d-%H%M%S)"
		cp -p -- "$codex_config" "$backup"
		chmod 600 "$backup" 2>/dev/null || true
		BIFROST_CODEX_VK="$key" write_codex_quickstart "$codex_config" "$base_url" >"$temporary"
	else
		BIFROST_CODEX_VK="$key" write_codex_quickstart /dev/null "$base_url" >"$temporary"
	fi
	chmod 600 "$temporary"
	mv -f -- "$temporary" "$codex_config"
	echo "Installed Codex provider in ${codex_config}"
	if [[ -n "${backup:-}" ]]; then
		echo "Backup: ${backup}"
	fi
	echo "New threads use gpt-5.6-sol with medium reasoning through ${base_url}"
	echo "Fully quit Codex and open a new task. Existing tasks keep their previous provider."
}

print_codex_hint() {
	local command
	if [[ -f "$0" ]]; then
		command="bash $(printf '%q' "$0") --configure-codex --address $(printf '%q' "$address")"
	else
		command="curl -fsSL https://github.com/${repository}/releases/latest/download/setup-binary.sh | bash -s -- --configure-codex --address $(printf '%q' "$address")"
	fi
	cat <<EOF

Codex does not use the router until its config.toml selects bifrost-router.
Run this once. It backs up that file, writes the provider, and does not print the virtual key.
If bifrost-router already points at this address, the file is left unchanged.

${command}

Then fully quit Codex and open a new task. Existing tasks keep their previous provider.
EOF
}

if [[ "$configure_codex" -eq 1 ]]; then
	install_codex_provider
	exit 0
fi

asset="bifrost-model-router-server-${goos}-${goarch}"
if [[ "$version" == "latest" ]]; then
	base="https://github.com/${repository}/releases/latest/download"
else
	base="https://github.com/${repository}/releases/download/${version}"
fi

temp_dir="$(mktemp -d "${TMPDIR:-/tmp}/bifrost-model-router.XXXXXX")"
cleanup() {
	rm -rf "$temp_dir"
}
trap cleanup EXIT

downloaded="${temp_dir}/${asset}"
sums_file="${temp_dir}/SHA256SUMS"
echo "Downloading ${base}/${asset}"
if ! curl -fsSL "${base}/${asset}" -o "$downloaded"; then
	if [[ "$goos" == "darwin" ]]; then
		echo "No macOS binary is published. On a Mac, run: mise run build:darwin" >&2
	else
		echo "Download failed for ${asset}" >&2
	fi
	exit 1
fi
curl -fsSL "${base}/SHA256SUMS" -o "$sums_file"

expected="$(awk -v name="$asset" '$2 == name { print $1 }' "$sums_file")"
if [[ -z "$expected" ]]; then
	echo "SHA256SUMS has no entry for ${asset}" >&2
	exit 1
fi
if command -v sha256sum >/dev/null 2>&1; then
	actual="$(sha256sum "$downloaded" | awk '{ print $1 }')"
else
	actual="$(shasum -a 256 "$downloaded" | awk '{ print $1 }')"
fi
if [[ "$actual" != "$expected" ]]; then
	echo "checksum mismatch for ${asset}" >&2
	exit 1
fi

install_dir="${XDG_DATA_HOME:-${HOME}/.local/share}/bifrost-model-router"
binary="${install_dir}/bin/bifrost-model-router-server"
if [[ "$goos" == "darwin" ]]; then
	config_file="${HOME}/Library/Application Support/bifrost-model-router/config.json"
else
	config_file="${XDG_CONFIG_HOME:-${HOME}/.config}/bifrost-model-router/config.json"
fi

if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
	systemctl --user stop bifrost-model-router.service >/dev/null 2>&1 || true
fi
if command -v pkill >/dev/null 2>&1; then
	# Match the installed path. The process name itself is truncated to 15
	# characters, so an exact comm match does not see this binary.
	pkill -f -- "$binary" >/dev/null 2>&1 || true
	sleep 1
fi

mkdir -p "${install_dir}/bin"
install -m 0755 "$downloaded" "$binary"

if [[ ! -f "$config_file" ]]; then
	echo "Installed ${binary}"
	echo "Create ${config_file}, then run this script again to start the router."
	print_codex_hint
	exit 0
fi

health_host="${address%%:*}"
health_port="${address##*:}"
health_url="http://${health_host}:${health_port}/health"
log_file="${install_dir}/router.log"

if command -v systemctl >/dev/null 2>&1 && systemctl --user show-environment >/dev/null 2>&1; then
	unit_dir="${XDG_CONFIG_HOME:-${HOME}/.config}/systemd/user"
	mkdir -p "$unit_dir"
	cat >"${unit_dir}/bifrost-model-router.service" <<EOF
[Unit]
Description=Bifrost Model Router

[Service]
ExecStart="${binary}" -addr ${address}
Restart=on-failure
RestartSec=2

[Install]
WantedBy=default.target
EOF
	systemctl --user daemon-reload
	systemctl --user enable --now bifrost-model-router.service
else
	nohup "$binary" -addr "$address" >>"$log_file" 2>&1 &
	echo "Started without systemd. It will not come back at the next login."
fi

for _ in $(seq 1 120); do
	if curl --fail --silent --max-time 2 "$health_url" >/dev/null; then
		echo "Installed ${binary}"
		echo "Router healthy at ${health_url}"
		print_codex_hint
		exit 0
	fi
	sleep 1
done

echo "Router did not become healthy; see ${log_file}" >&2
if command -v journalctl >/dev/null 2>&1; then
	echo "systemd log: journalctl --user -u bifrost-model-router.service" >&2
fi
exit 1
