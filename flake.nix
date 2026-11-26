{
  description = "Meals — Minimalist meal planner and shopping list";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
    kit = {
      url = "github:bhr-tools/kit";
      inputs.nixpkgs.follows = "nixpkgs";
    };
  };

  outputs = { self, nixpkgs, flake-utils, kit }:
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

      # Multi-instance NixOS module, shared by the apps (kit/nix/app-module.nix).
      nixosModule = kit.lib.mkAppModule {
        name = "meals";
        title = "Meals";
        package = pkgs: self.packages.${pkgs.stdenv.hostPlatform.system}.default;
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
