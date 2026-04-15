{
  inputs.nixpkgs.url = "github:nixos/nixpkgs/nixos-unstable";
  inputs.flake-utils.url = "github:numtide/flake-utils";
  inputs.git-hooks.url = "github:cachix/git-hooks.nix";
  inputs.git-hooks.inputs.nixpkgs.follows = "nixpkgs";

  outputs = {
    self,
    nixpkgs,
    flake-utils,
    git-hooks,
  }:
    flake-utils.lib.eachDefaultSystem (system: let
      pkgs = nixpkgs.legacyPackages.${system};
    in {
      formatter = pkgs.alejandra;

      checks.pre-commit = git-hooks.lib.${system}.run {
        src = ./.;
        hooks = {
          gotest.enable = true;
          govet.enable = true;
          alejandra.enable = true;
          golangci-lint = {
            enable = true;
            name = "golangci-lint";
            entry = "${pkgs.golangci-lint}/bin/golangci-lint fmt";
            types = ["go"];
          };

          nix-build = {
            enable = true;
            name = "nix-build";
            entry = pkgs.lib.getExe (pkgs.writeShellApplication {
              name = "nix-build-check";
              runtimeInputs = [pkgs.nix];
              text = "nix build --no-link";
            });
            stages = ["pre-push"];
            pass_filenames = false;
            files = "go\\.(mod|sum)|flake\\.nix";
          };
        };
      };

      packages.default = pkgs.buildGoModule {
        pname = "null-statement-parser";
        version = self.shortRev or self.dirtyShortRev or "dev";
        src = ./.;
        vendorHash = "sha256-j1B9wTOC8E5eYyPRhfi4GHyz/iiRY4MlG1Yg3lNugpI=";
        subPackages = ["cmd"];
      };

      devShells.default = pkgs.mkShell {
        shellHook = self.checks.${system}.pre-commit.shellHook;
        env.UV_CACHE_DIR = ".uv-cache";
        packages = with pkgs; [
          go
          python3
          buf
          ruff
          uv
          golangci-lint

          protoc-gen-go-grpc
          protoc-gen-go

          (writeShellScriptBin "regen" ''
            rm -rf internal/gen/
            ${buf}/bin/buf generate
          '')

          (writeShellScriptBin "run" ''
            go run cmd/main.go
          '')

          (writeShellScriptBin "fmt" ''
            ${golangci-lint}/bin/golangci-lint fmt
          '')

          (writeShellScriptBin "bump-protos" ''
            git -C proto fetch origin
            git -C proto checkout main
            git -C proto pull --ff-only
            git add proto
            git commit -m "chore: bump proto files"
            git push
          '')
        ];

        # Required for pymupdf binary dependencies
        LD_LIBRARY_PATH = pkgs.lib.makeLibraryPath [
          pkgs.stdenv.cc.cc.lib
          pkgs.zlib
        ];
      };
    });
}
