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

Downloads the native server for this machine from the GitHub release and
installs it under ${XDG_DATA_HOME:-$HOME/.local/share}/bifrost-model-router.
The default listen address is 127.0.0.1:8080. Point Codex base_url at
http://127.0.0.1:8080/v1, or pass --address 127.0.0.1:80 when that port is
available. macOS release binaries are not published; build them on a Mac.
EOF
}

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
		echo "Set the Codex provider base_url to http://${health_host}:${health_port}/v1"
		exit 0
	fi
	sleep 1
done

echo "Router did not become healthy; see ${log_file}" >&2
if command -v journalctl >/dev/null 2>&1; then
	echo "systemd log: journalctl --user -u bifrost-model-router.service" >&2
fi
exit 1
