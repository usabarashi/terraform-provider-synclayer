{
  description = "Terraform provider for SyncLayer CPE devices (SXEP200W)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
  };

  outputs = { self, nixpkgs }:
    let
      systems = [
        "x86_64-linux"
        "aarch64-linux"
        "x86_64-darwin"
        "aarch64-darwin"
      ];
      forAllSystems = f:
        nixpkgs.lib.genAttrs systems (system: f {
          # terraform is distributed under the BUSL license and requires
          # allowUnfree to be available in the dev shell.
          pkgs = import nixpkgs {
            inherit system;
            config.allowUnfree = true;
          };
        });
    in
    {
      devShells = forAllSystems ({ pkgs }: {
        default = pkgs.mkShell {
          packages = with pkgs; [
            go
            gopls
            gotools
            terraform
            terraform-docs
          ];

          shellHook = ''
            echo "terraform-provider-synclayer dev shell"
            echo "  go $(go version | cut -d' ' -f3)"
            echo "  terraform $(terraform version | head -n1 | cut -d' ' -f2)"
          '';
        };
      });

      # `nix build` produces the provider binary in ./result/bin.
      packages = forAllSystems ({ pkgs }: {
        default = pkgs.buildGoModule {
          pname = "terraform-provider-synclayer";
          version = "0.1.0";
          src = ./.;
          vendorHash = "sha256-UCLOvJqLuyIXuGuX0913YtPvo1HthxN4wm0tRqjKRfg=";
          subPackages = [ "." ];
        };
      });
    };
}
