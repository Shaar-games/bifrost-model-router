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
            vendorHash = "sha256-Pvpgztn+0kKrqkqtmNNRs4IpdVpyjoNk2zd2UiRxNM8=";
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
        in
        {
          inherit plugin;
          config-check = configCheck;
          default = pkgs.symlinkJoin {
            name = "bifrost-model-router-${version}";
            paths = [
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

      # Kept as an input in the lock file so the host revision paired with this
      # plugin is explicit. The coupled host build is added after the ABI smoke
      # test in the next milestone.
      _bifrostSource = bifrost;
    };
}
