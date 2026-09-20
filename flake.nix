{
  description = "Provider-agnostic Bifrost model router for Codex";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    systems.url = "github:nix-systems/default-linux";
    bifrost = {
      url = "github:maximhq/bifrost/ed79592fc4771f12f2717dd7c9ab668e663a08f3";
      flake = false;
    };
  };

  outputs =
    {
      self,
      nixpkgs,
      systems,
      bifrost,
    }:
    let
      eachSystem = nixpkgs.lib.genAttrs (import systems);
    in
    {
      packages = eachSystem (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
          version = "0.1.0-dev";
          bifrostUI = pkgs.buildNpmPackage {
            pname = "bifrost-ui";
            inherit version;
            src = "${bifrost}/ui";
            npmDepsHash = "sha256-cOswnT4ZahWX66h9oiw4t3r5GZeOH/yjbnTCAsjVgnw=";
            npmBuildScript = "build-enterprise";
            installPhase = ''
              runHook preInstall
              cp -R out $out
              runHook postInstall
            '';
          };
          common = {
            pname = "bifrost-model-router";
            inherit version;
            src = self;
            vendorHash = "sha256-4louYB9FqGkC/FyN6LY/1TtcvGxaZMucvm19lNDRAZI=";
            nativeBuildInputs = [ pkgs.pkg-config ];
            buildInputs = [ pkgs.sqlite ];
            go = pkgs.go_1_27;
            doCheck = true;
          };
          configCheck = pkgs.buildGo127Module (
            common
            // {
              pname = "bifrost-router-config-check";
              subPackages = [ "cmd/config-check" ];
            }
          );
          router = pkgs.buildGo127Module (
            common
            // {
              pname = "bifrost-router-cli";
              subPackages = [ "cmd/router" ];
              ldflags = [ "-X main.version=${version}" ];
            }
          );
          gateway = pkgs.buildGo127Module (
            common
            // {
              pname = "bifrost-router-gateway";
              subPackages = [ "cmd/gateway" ];
            }
          );
          mockProvider = pkgs.buildGo127Module (
            common
            // {
              pname = "bifrost-router-mock-provider";
              subPackages = [ "cmd/mock-provider" ];
              doCheck = false;
            }
          );
          plugin = pkgs.buildGo127Module (
            common
            // {
              pname = "codex-model-router-plugin";
              doCheck = false;
              buildPhase = ''
                runHook preBuild
                CGO_ENABLED=1 go build -buildmode=plugin -trimpath \
                  -o codex-model-router.so ./plugins/codex-router
                runHook postBuild
              '';
              installPhase = ''
                runHook preInstall
                install -Dm755 codex-model-router.so \
                  $out/lib/bifrost/plugins/codex-model-router.so
                runHook postInstall
              '';
            }
          );
          bifrostHost = pkgs.buildGo127Module {
            pname = "bifrost-http";
            inherit version;
            src = bifrost;
            modRoot = "transports";
            vendorHash = "sha256-tlKIt38Rh5KQu4ny534eqC6PPE1DTLQEC5m+rHAUJJ0=";
            subPackages = [ "bifrost-http" ];
            tags = [ "sqlite_static" ];
            postPatch = ''
              cp -R ${bifrostUI} transports/bifrost-http/ui
            '';
            nativeBuildInputs = [ pkgs.pkg-config ];
            buildInputs = [ pkgs.sqlite ];
            go = pkgs.go_1_27;
            ldflags = [
              "-s"
              "-w"
              "-X main.Version=v2.2.1-router"
            ];
            doCheck = false;
          };
          launcher = pkgs.writeShellApplication {
            name = "bifrost-model-router-server";
            runtimeInputs = [ pkgs.coreutils ];
            text = ''
              app_dir="''${BIFROST_APP_DIR:-/var/lib/bifrost}"
              config_file="''${BIFROST_CONFIG_FILE:-/etc/bifrost/config.json}"
              mkdir -p "$app_dir"
              install -m 0600 "$config_file" "$app_dir/config.json"
              exec ${bifrostHost}/bin/bifrost-http \
                -app-dir "$app_dir" \
                -host "''${BIFROST_HOST:-127.0.0.1}" \
                -port "''${BIFROST_PORT:-8080}"
            '';
          };
          imageRoot = pkgs.buildEnv {
            name = "bifrost-model-router-image-root";
            paths = [
              bifrostHost
              plugin
              gateway
              launcher
              pkgs.cacert
              pkgs.curl
              pkgs.tzdata
            ];
            pathsToLink = [
              "/bin"
              "/etc"
              "/lib"
              "/share"
            ];
          };
          image = pkgs.dockerTools.streamLayeredImage {
            name = "bifrost-model-router";
            tag = "nix";
            contents = [ imageRoot ];
            config = {
              Entrypoint = [ "/bin/bifrost-model-router-server" ];
              Env = [
                "BIFROST_APP_DIR=/var/lib/bifrost"
                "BIFROST_CONFIG_FILE=/etc/bifrost/config.json"
                "BIFROST_HOST=127.0.0.1"
                "BIFROST_PORT=8080"
                "SSL_CERT_FILE=/etc/ssl/certs/ca-bundle.crt"
                "TZDIR=/share/zoneinfo"
              ];
              User = "65532:65532";
              WorkingDir = "/var/lib/bifrost";
              ExposedPorts = {
                "8080/tcp" = { };
              };
              Labels = {
                "org.opencontainers.image.source" = "https://github.com/applyinnovations/bifrost-model-router";
                "org.opencontainers.image.version" = version;
                "org.opencontainers.image.title" = "Bifrost Model Router";
              };
            };
          };
        in
        {
          inherit plugin image gateway;
          bifrost = bifrostHost;
          bifrost-ui = bifrostUI;
          bifrost-model-router-image = image;
          config-check = configCheck;
          inherit router;
          mock-provider = mockProvider;
          default = pkgs.symlinkJoin {
            name = "bifrost-model-router-${version}";
            paths = [
              bifrostHost
              plugin
              configCheck
              router
              gateway
            ];
          };
        }
      );

      checks = eachSystem (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          inherit (self.packages.${system}) plugin config-check;
          bifrost = self.packages.${system}.bifrost;
          bifrost-ui = self.packages.${system}.bifrost-ui;
          image = self.packages.${system}.bifrost-model-router-image;
          image-metadata =
            pkgs.runCommand "bifrost-router-image-metadata"
              {
                nativeBuildInputs = [
                  pkgs.gnutar
                  pkgs.jq
                ];
              }
              ''
                work=$TMPDIR/image
                mkdir -p "$work"
                ${self.packages.${system}.bifrost-model-router-image} > "$work/image.tar"
                tar -xf "$work/image.tar" -C "$work" manifest.json
                config_name=$(jq -r '.[0].Config' "$work/manifest.json")
                tar -xf "$work/image.tar" -C "$work" "$config_name"
                jq -e '.config.User == "65532:65532"' "$work/$config_name" >/dev/null
                jq -e '.config.Entrypoint == ["/bin/bifrost-model-router-server"]' "$work/$config_name" >/dev/null
                jq -e '.config.WorkingDir == "/var/lib/bifrost"' "$work/$config_name" >/dev/null
                touch $out
              '';
          ui-content =
            pkgs.runCommand "bifrost-router-ui-content"
              {
                nativeBuildInputs = [ pkgs.ripgrep ];
              }
              ''
                test -s ${self.packages.${system}.bifrost-ui}/index.html
                test -d ${self.packages.${system}.bifrost-ui}/assets
                ! rg -q "API-only build" ${self.packages.${system}.bifrost-ui}/index.html
                touch $out
              '';
          format =
            pkgs.runCommand "bifrost-router-format"
              {
                nativeBuildInputs = [
                  pkgs.go_1_27
                  pkgs.jq
                  pkgs.nixfmt
                  pkgs.shellcheck
                  pkgs.shfmt
                ];
              }
              ''
                cp -R ${self} source
                chmod -R u+w source
                cd source
                test -z "$(gofmt -l cmd internal plugins)"
                nixfmt --check flake.nix nix/*.nix
                shfmt -d scripts
                shellcheck scripts/*.sh
                jq empty config/*.json
                touch $out
              '';
          lint =
            pkgs.runCommand "bifrost-router-lint"
              {
                nativeBuildInputs = [
                  pkgs.go_1_27
                  pkgs.pkg-config
                  pkgs.sqlite
                  pkgs.stdenv.cc
                ];
              }
              ''
                export GOCACHE=$TMPDIR/go-cache
                export GOPATH=$TMPDIR/go
                cp -R ${self} source
                chmod -R u+w source
                cp -R ${self.packages.${system}.plugin.goModules} source/vendor
                cd source
                go vet ./...
                touch $out
              '';
          schema =
            pkgs.runCommand "bifrost-router-schema"
              {
                nativeBuildInputs = [
                  pkgs.check-jsonschema
                  self.packages.${system}.config-check
                ];
              }
              ''
                check-jsonschema --schemafile ${./config/router.schema.json} ${./config/router.example.yaml}
                config-check ${./config/router.example.yaml} >/dev/null
                touch $out
              '';
          race =
            pkgs.runCommand "bifrost-router-race"
              {
                nativeBuildInputs = [
                  pkgs.go_1_27
                  pkgs.pkg-config
                  pkgs.sqlite
                  pkgs.stdenv.cc
                ];
              }
              ''
                export GOCACHE=$TMPDIR/go-cache
                export GOPATH=$TMPDIR/go
                cp -R ${self} source
                chmod -R u+w source
                cp -R ${self.packages.${system}.plugin.goModules} source/vendor
                cd source
                go test -race ./...
                touch $out
              '';
          fuzz-smoke =
            pkgs.runCommand "bifrost-router-fuzz-smoke"
              {
                nativeBuildInputs = [
                  pkgs.go_1_27
                  pkgs.pkg-config
                  pkgs.sqlite
                  pkgs.stdenv.cc
                ];
              }
              ''
                export GOCACHE=$TMPDIR/go-cache
                export GOPATH=$TMPDIR/go
                cp -R ${self} source
                chmod -R u+w source
                cp -R ${self.packages.${system}.plugin.goModules} source/vendor
                cd source
                go test ./internal/catalog -run '^$' -fuzz FuzzHydrateDeterministic -fuzztime 2s
                touch $out
              '';
          go-test =
            pkgs.runCommand "bifrost-router-go-test"
              {
                nativeBuildInputs = [
                  pkgs.go_1_27
                  pkgs.pkg-config
                  pkgs.sqlite
                  pkgs.stdenv.cc
                ];
              }
              ''
                export GOCACHE=$TMPDIR/go-cache
                export GOPATH=$TMPDIR/go
                cp -R ${self} source
                chmod -R u+w source
                cp -R ${self.packages.${system}.plugin.goModules} source/vendor
                cd source
                go test ./...
                touch $out
              '';
          plugin-load =
            pkgs.runCommand "bifrost-router-plugin-load"
              {
                nativeBuildInputs = [ pkgs.curl ];
              }
              ''
                mkdir -p $TMPDIR/app
                cat >$TMPDIR/app/pricing.json <<JSON
                {}
                JSON
                cat >$TMPDIR/app/model-parameters.json <<JSON
                {}
                JSON
                cat >$TMPDIR/app/config.json <<JSON
                {
                  "\$schema": "https://www.getbifrost.ai/schema",
                  "version": 2,
                  "config_store": { "enabled": false },
                  "logs_store": { "enabled": false },
                  "framework": {
                    "pricing": {
                      "pricing_url": "file://$TMPDIR/app/pricing.json",
                      "model_parameters_url": "file://$TMPDIR/app/model-parameters.json"
                    }
                  },
                  "client": { "allow_direct_keys": true, "disable_content_logging": true },
                  "providers": { "openai": { "keys": [] } },
                  "plugins": [{
                    "enabled": true,
                    "name": "codex-model-router",
                    "path": "${self.packages.${system}.plugin}/lib/bifrost/plugins/codex-model-router.so",
                    "config": {
                      "version": 1,
                      "providers": {
                        "openai": { "credential_mode": "request_passthrough", "responses_mode": "native" }
                      },
                      "models": { "openai/test": { "codex": {} } }
                    }
                  }]
                }
                JSON
                ${self.packages.${system}.bifrost}/bin/bifrost-http \
                  -app-dir $TMPDIR/app -host 127.0.0.1 -port 18080 \
                  >$TMPDIR/bifrost.log 2>&1 &
                server_pid=$!
                trap 'kill $server_pid 2>/dev/null || true' EXIT
                ready=0
                for attempt in $(seq 1 60); do
                  if curl --fail --silent http://127.0.0.1:18080/health >/dev/null; then
                    ready=1
                    break
                  fi
                  if ! kill -0 $server_pid 2>/dev/null; then
                    cat $TMPDIR/bifrost.log >&2
                    exit 1
                  fi
                  sleep 0.25
                done
                if [ "$ready" -ne 1 ]; then
                  cat $TMPDIR/bifrost.log >&2
                  exit 1
                fi
                touch $out
              '';
          e2e =
            pkgs.runCommand "bifrost-router-e2e"
              {
                nativeBuildInputs = [
                  pkgs.bash
                  pkgs.curl
                  pkgs.jq
                ];
              }
              ''
                export BIFROST_BIN=${self.packages.${system}.bifrost}/bin/bifrost-http
                export ROUTER_PLUGIN=${self.packages.${system}.plugin}/lib/bifrost/plugins/codex-model-router.so
                export MOCK_PROVIDER_BIN=${self.packages.${system}.mock-provider}/bin/mock-provider
                ${pkgs.bash}/bin/bash ${./scripts/e2e.sh}
                touch $out
              '';
        }
      );

      apps = eachSystem (system: {
        config-check = {
          type = "app";
          program = "${self.packages.${system}.config-check}/bin/config-check";
        };
        default = {
          type = "app";
          program = "${self.packages.${system}.router}/bin/router";
        };
        router = {
          type = "app";
          program = "${self.packages.${system}.router}/bin/router";
        };
        gateway = {
          type = "app";
          program = "${self.packages.${system}.gateway}/bin/gateway";
        };
        bifrost = {
          type = "app";
          program = "${self.packages.${system}.bifrost}/bin/bifrost-http";
        };
      });

      devShells = eachSystem (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          default = pkgs.mkShell {
            packages = with pkgs; [
              go_1_27
              gopls
              gotools
              gotestsum
              golangci-lint
              govulncheck
              just
              jq
              yq-go
              shellcheck
              shfmt
              nixfmt
              pkg-config
              sqlite
            ];
          };
        }
      );

      formatter = eachSystem (
        system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        pkgs.writeShellApplication {
          name = "bifrost-router-format-nix";
          runtimeInputs = [ pkgs.nixfmt ];
          text = ''
            if (( $# > 0 )); then
              exec nixfmt "$@"
            fi
            mapfile -d "" files < <(find . -type f -name '*.nix' -not -path './.git/*' -print0)
            nixfmt "''${files[@]}"
          '';
        }
      );

      nixosModules.default = import ./nix/module.nix self;

      _bifrostSource = bifrost;
    };
}
