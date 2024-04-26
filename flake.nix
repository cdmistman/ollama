{
  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixpkgs-unstable";

    devshell = {
      url = "github:numtide/devshell";
      inputs.flake-utils.follows = "flake-utils";
      inputs.nixpkgs.follows = "nixpkgs";
    };

    flake-parts = {
      url = "github:cdmistman/flake-parts/all-modules";
    };

    flake-utils = {
      url = "github:numtide/flake-utils";
      inputs.systems.follows = "systems";
    };

    systems.url = "github:nix-systems/default";
  };

  outputs = inputs: inputs.flake-parts.lib.mkFlake {inherit inputs;} {
    systems = import inputs.systems;

    imports = with inputs; [
      devshell.flakeModule
    ];

    perSystem = { pkgs, ... }: {
      devshells.default = {
        packages = [
          pkgs.cmake
          pkgs.go
        ];
      };
    };
  };
}
