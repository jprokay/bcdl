{
  description = "Go development environment";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        goVersion = 22;
        overlays = [ (final: prev: { go = prev."go_1_${toString goVersion}"; }) ];
        pkgs = import nixpkgs {
          inherit overlays system;
        };

      in {
        devShell = pkgs.mkShell {
          packages = with pkgs; [
            go

            gotools

            golangci-lint
          ];
        };
      }
    );
}
