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
              cp -R ${./nix/ui} transports/bifrost-http/ui
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
        in
        {
          inherit plugin;
          bifrost = bifrostHost;
          config-check = configCheck;
          mock-provider = mockProvider;
          default = pkgs.symlinkJoin {
            name = "bifrost-model-router-${version}";
            paths = [
              bifrostHost
              plugin
              configCheck
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
          go-test =
            pkgs.runCommand "bifrost-router-go-test"
              {
                nativeBuildInputs = [ pkgs.go_1_27 ];
              }
              ''
                export HOME=$TMPDIR
                export GOCACHE=$TMPDIR/go-cache
                export GOPATH=$TMPDIR/go
                cp -R ${self} source
                chmod -R u+w source
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
                export HOME=$TMPDIR
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
          program = "${self.packages.${system}.config-check}/bin/config-check";
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
              staticcheck
              govulncheck
              just
              jq
              yq-go
              shellcheck
              shfmt
              nixfmt-rfc-style
              pkg-config
              sqlite
            ];
          };
        }
      );

      formatter = eachSystem (system: (import nixpkgs { inherit system; }).nixfmt-rfc-style);

      _bifrostSource = bifrost;
    };
}
