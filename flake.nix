{
  description = "Meals — Minimalist meal planner and shopping list";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    let
      # Supported systems
      supportedSystems = [ "x86_64-linux" "aarch64-linux" ];

      # Package definitions per architecture
      systemOutputs = flake-utils.lib.eachSystem supportedSystems (system:
        let
          pkgs = import nixpkgs { inherit system; };
        in
        {
          packages = rec {
            meals = pkgs.buildGoModule {
              pname = "meals";
              version = "1.0.0";
              src = ./.;

              subPackages = [ "cmd/server" ];
              vendorHash = null;

              postInstall = ''
                if [ -f "$out/bin/server" ]; then
                  mv "$out/bin/server" "$out/bin/meals"
                fi
              '';

              meta = with pkgs.lib; {
                description = "Meals — Minimalist meal planner and shopping list";
                homepage = "https://github.com/bhr-tools/Meals";
                license = licenses.mit;
                mainProgram = "meals";
              };
            };
            default = meals;
          };

          apps = rec {
            meals = flake-utils.lib.mkApp {
              drv = self.packages.${system}.meals;
              name = "meals";
            };
            default = meals;
          };

          devShells.default = pkgs.mkShell {
            buildInputs = with pkgs; [
              go
              gopls
              sqlite
            ];
          };
        }
      );

      # Native multi-instance NixOS module for server / desktop
      nixosModule = { config, lib, pkgs, ... }:
        let
          cfg = config.services.meals;
        in
        {
          options.services.meals = {
            enable = lib.mkEnableOption "Meals Server & Web Daemon";

            package = lib.mkOption {
              type = lib.types.package;
              default = self.packages.${pkgs.stdenv.hostPlatform.system}.default;
              description = "The Meals package to use.";
            };

            instances = lib.mkOption {
              type = lib.types.attrsOf (lib.types.submodule ({ name, config, ... }: {
                options = {
                  port = lib.mkOption {
                    type = lib.types.port;
                    description = "HTTP listening port for this Meals instance.";
                  };

                  host = lib.mkOption {
                    type = lib.types.str;
                    default = "127.0.0.1";
                    description = "IP address on which the instance listens.";
                  };

                  dataDir = lib.mkOption {
                    type = lib.types.path;
                    default = "/var/lib/meals-${name}";
                    description = "Data directory of the Meals instance.";
                  };

                  dbPath = lib.mkOption {
                    type = lib.types.path;
                    default = "${config.dataDir}/meals.db";
                    description = "Path to the SQLite database for this instance.";
                  };

                  noAuth = lib.mkOption {
                    type = lib.types.bool;
                    default = false;
                    description = "Disable password authentication.";
                  };

                  user = lib.mkOption {
                    type = lib.types.str;
                    default = "meals";
                    description = "System user under which to run the instance.";
                  };

                  group = lib.mkOption {
                    type = lib.types.str;
                    default = "meals";
                    description = "System group under which to run the instance.";
                  };

                  openFirewall = lib.mkOption {
                    type = lib.types.bool;
                    default = false;
                    description = "Automatically open the port in the NixOS firewall.";
                  };
                };
              }));
              default = {};
              description = "Definition of independent Meals instances.";
            };
          };

          config = lib.mkIf (cfg.enable && cfg.instances != {}) {
            networking.firewall.allowedTCPPorts = lib.concatLists (
              lib.mapAttrsToList (_: inst: lib.optional inst.openFirewall inst.port) cfg.instances
            );

            users.users = lib.mkIf (lib.any (inst: inst.user == "meals") (lib.attrValues cfg.instances)) {
              meals = {
                isSystemUser = true;
                group = "meals";
                home = "/var/lib/meals";
                createHome = true;
                description = "Meals daemon user";
              };
            };

            users.groups = lib.mkIf (lib.any (inst: inst.group == "meals") (lib.attrValues cfg.instances)) {
              meals = {};
            };

            systemd.services = lib.mapAttrs' (name: inst:
              let
                serviceName = if name == "default" then "meals" else "meals-${name}";
              in
              lib.nameValuePair serviceName {
                description = "Meals Daemon (${name})";
                after = [ "network.target" ];
                wantedBy = [ "multi-user.target" ];

                serviceConfig = {
                  ExecStart = "${cfg.package}/bin/meals -host ${inst.host} -port ${toString inst.port} -data-dir ${toString inst.dataDir} -db ${toString inst.dbPath}"
                    + lib.optionalString inst.noAuth " -no-auth";
                  Restart = "always";
                  RestartSec = 3;
                  User = inst.user;
                  Group = inst.group;
                  WorkingDirectory = toString inst.dataDir;
                  ReadWritePaths = [ (toString inst.dataDir) ];
                  # Hardening: the service only needs its data directory and the network.
                  NoNewPrivileges = true;
                  PrivateTmp = true;
                  PrivateDevices = true;
                  ProtectSystem = "strict";
                  ProtectHome = true;
                  ProtectKernelTunables = true;
                  ProtectKernelModules = true;
                  ProtectControlGroups = true;
                  RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
                  RestrictNamespaces = true;
                  LockPersonality = true;
                  MemoryDenyWriteExecute = true;
                  SystemCallArchitectures = "native";
                } // lib.optionalAttrs (lib.hasPrefix "/var/lib/" (toString inst.dataDir)) {
                  # systemd creates and owns the directory when it lives under /var/lib.
                  StateDirectory = lib.removePrefix "/var/lib/" (toString inst.dataDir);
                };
              }
            ) cfg.instances;
          };
        };
    in
    systemOutputs // {
      nixosModules.default = nixosModule;
      nixosModules.meals = nixosModule;
      overlays.default = final: prev: {
        meals = self.packages.${final.stdenv.hostPlatform.system}.default;
      };
    };
}
