self:
{
  config,
  lib,
  pkgs,
  ...
}:
let
  inherit (lib)
    concatMapStringsSep
    escapeShellArg
    mapAttrsToList
    mkEnableOption
    mkIf
    mkOption
    types
    ;
  cfg = config.services.bifrost-model-router;
  statePath = "/var/lib/${cfg.stateDirectory}";
  credentialNames = builtins.attrNames cfg.credentials;
  validCredentialName = name: builtins.match "[A-Za-z_][A-Za-z0-9_]*" name != null;
  launcher = pkgs.writeShellScript "bifrost-model-router-start" ''
    set -euo pipefail
    ${concatMapStringsSep "\n" (name: ''
      export ${name}="$(<"$CREDENTIALS_DIRECTORY/${name}")"
    '') credentialNames}
    exec ${cfg.package}/bin/bifrost-http \
      -app-dir ${escapeShellArg statePath} \
      -host ${escapeShellArg cfg.listenAddress} \
      -port ${toString cfg.port} \
      ${concatMapStringsSep " " escapeShellArg cfg.extraArgs}
  '';
in
{
  options.services.bifrost-model-router = {
    enable = mkEnableOption "the Bifrost Codex model router";
    package = mkOption {
      type = types.package;
      default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
      description = "Router package built with the ABI-matched Bifrost host and plugin.";
    };
    configFile = mkOption {
      type = types.path;
      description = "Bifrost config.json containing the codex-model-router plugin configuration.";
    };
    listenAddress = mkOption {
      type = types.str;
      default = "127.0.0.1";
    };
    port = mkOption {
      type = types.port;
      default = 8080;
    };
    stateDirectory = mkOption {
      type = types.strMatching "[A-Za-z0-9_.-]+";
      default = "bifrost-model-router";
      description = "Name of the systemd-managed directory below /var/lib.";
    };
    environmentFiles = mkOption {
      type = types.listOf types.path;
      default = [ ];
      description = "Optional systemd EnvironmentFile inputs; prefer credentials for secrets.";
    };
    credentials = mkOption {
      type = types.attrsOf types.path;
      default = { };
      example = {
        CEREBRAS_API_KEY = "/run/secrets/cerebras-api-key";
      };
      description = "Environment variable names mapped to files loaded through systemd credentials.";
    };
    extraArgs = mkOption {
      type = types.listOf types.str;
      default = [ ];
    };
  };

  config = mkIf cfg.enable {
    assertions = [
      {
        assertion = builtins.all validCredentialName credentialNames;
        message = "services.bifrost-model-router.credentials keys must be valid environment variable names";
      }
    ];

    systemd.services.bifrost-model-router = {
      description = "Bifrost Codex model router";
      wantedBy = [ "multi-user.target" ];
      after = [ "network-online.target" ];
      wants = [ "network-online.target" ];
      environment.HOME = statePath;
      preStart = ''
        install -m 0600 ${cfg.configFile} ${statePath}/config.json
      '';
      serviceConfig = {
        ExecStart = launcher;
        DynamicUser = true;
        StateDirectory = cfg.stateDirectory;
        WorkingDirectory = statePath;
        EnvironmentFile = cfg.environmentFiles;
        LoadCredential = mapAttrsToList (name: path: "${name}:${toString path}") cfg.credentials;
        Restart = "on-failure";
        RestartSec = "2s";
        TimeoutStopSec = "30s";
        NoNewPrivileges = true;
        PrivateTmp = true;
        PrivateDevices = true;
        ProtectSystem = "strict";
        ProtectHome = true;
        ProtectKernelTunables = true;
        ProtectKernelModules = true;
        ProtectControlGroups = true;
        RestrictAddressFamilies = [
          "AF_UNIX"
          "AF_INET"
          "AF_INET6"
        ];
        RestrictSUIDSGID = true;
        LockPersonality = true;
        MemoryDenyWriteExecute = false;
        CapabilityBoundingSet = "";
        SystemCallArchitectures = "native";
        UMask = "0077";
      };
    };
  };
}
