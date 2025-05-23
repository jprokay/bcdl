{ pkgs, lib, config, inputs, ... }:

let
  pkgs-unstable = import inputs.nixpkgs-unstable { system = pkgs.stdenv.system; };
in {
  # https://devenv.sh/basics/

  # https://devenv.sh/packages/
  packages = [
    pkgs-unstable.go
    pkgs-unstable.gopls
    pkgs-unstable.golangci-lint
    pkgs.git
  ];

  env.GOPLS = "${pkgs-unstable.gopls}/bin/gopls";

  languages.go = {
    enable = true;
    package = pkgs-unstable.go;
  };
  # https://devenv.sh/scripts/
    # https://devenv.sh/services/
  # services.postgres.enable = true;

  # https://devenv.sh/languages/
  # languages.nix.enable = true;

  # https://devenv.sh/pre-commit-hooks/
  # pre-commit.hooks.shellcheck.enable = true;

  # https://devenv.sh/processes/
  # processes.ping.exec = "ping example.com";

  # See full reference at https://devenv.sh/reference/options/
}
