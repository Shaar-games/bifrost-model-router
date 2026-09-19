set shell := ["bash", "-euo", "pipefail", "-c"]

fmt:
    gofmt -w cmd internal plugins
    nix fmt

test:
    go test ./...

test-race:
    go test -race ./...

lint:
    go vet ./...

test-contract:
    go test ./internal/catalog ./internal/credentials ./internal/responses ./plugins/codex-router

test-e2e:
    nix build .#checks.x86_64-linux.e2e --no-link -L

run app_dir:
    nix run .#bifrost -- -app-dir {{app_dir}} -host 127.0.0.1 -port 8080

check-config:
    go run ./cmd/config-check ./config/router.example.yaml >/dev/null

install-codex-profile:
    go run ./cmd/router profile install

package:
    nix build .#default

check:
    nix flake check
