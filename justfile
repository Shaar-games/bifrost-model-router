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
    staticcheck ./...

check-config:
    go run ./cmd/config-check ./config/router.example.yaml >/dev/null

package:
    nix build .#default

check:
    nix flake check
