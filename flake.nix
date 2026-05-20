{
  description = "OpenClaw Dashboard — real-time bot monitoring UI (Go)";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        # Project requires Go 1.26+ (see go.mod). Pin explicitly so we don't
        # silently drift to whatever the nixpkgs channel default is.
        go = pkgs.go_1_26 or pkgs.go;
        runtimeDeps = [ pkgs.bash pkgs.git ];
        version = pkgs.lib.fileContents ./VERSION;
      in {
        packages = {
          # Go binary (default) — single binary, zero runtime deps
          default = pkgs.buildGoModule {
            pname = "openclaw-dashboard";
            inherit version;
            src = ./.;
            inherit go;
            vendorHash = null; # no external deps
            subPackages = [ "cmd/openclaw-dashboard" ];

            env.CGO_ENABLED = "0";
            ldflags = [
              "-s" "-w"
              "-X" "github.com/mudrii/openclaw-dashboard.BuildVersion=${version}"
            ];

            nativeBuildInputs = [ pkgs.makeWrapper ];

            postInstall = ''
              mkdir -p $out/share/openclaw-dashboard/examples
              cp ${./assets/runtime/refresh.sh} $out/share/openclaw-dashboard/refresh.sh
              cp ${./assets/runtime/themes.json} $out/share/openclaw-dashboard/themes.json
              cp ${./assets/runtime/config.json} $out/share/openclaw-dashboard/config.json
              cp ${./VERSION} $out/share/openclaw-dashboard/VERSION
              cp ${./examples/config.minimal.json} $out/share/openclaw-dashboard/examples/config.minimal.json
              cp ${./examples/config.full.json} $out/share/openclaw-dashboard/examples/config.full.json
              chmod +x $out/share/openclaw-dashboard/refresh.sh
              wrapProgram $out/bin/openclaw-dashboard \
                --prefix PATH : ${pkgs.lib.makeBinPath runtimeDeps}
            '';

            meta = {
              description = "OpenClaw real-time bot monitoring dashboard (Go)";
              license = pkgs.lib.licenses.mit;
              # Project targets Linux + macOS only (see .goreleaser.yml goos
              # list). Narrower than platforms.unix so `nix flake check`
              # surfaces a clean error on BSD/etc. instead of an opaque build
              # failure mid-compile.
              platforms = pkgs.lib.platforms.linux ++ pkgs.lib.platforms.darwin;
              mainProgram = "openclaw-dashboard";
            };
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = [
            go
            pkgs.bash pkgs.git
            pkgs.gopls pkgs.gotools pkgs.gofumpt
            pkgs.golangci-lint pkgs.govulncheck
          ];
          shellHook = ''
            echo "OpenClaw Dashboard dev shell"
            echo ""
            echo "  Go:     go run ./cmd/openclaw-dashboard --port 8080"
            echo "  Build:  go build -ldflags='-s -w' -o openclaw-dashboard ./cmd/openclaw-dashboard"
            echo "  Test:   go test -race -v -count=1 ./..."
            echo "  Vuln:   govulncheck ./..."
            echo "  Lint:   golangci-lint run"
          '';
        };

        apps = {
          default = flake-utils.lib.mkApp {
            drv = self.packages.${system}.default;
            exePath = "/bin/openclaw-dashboard";
          };
        };

        # `nix flake check` will build the default package on each system.
        checks = {
          build = self.packages.${system}.default;
        };
      });
}
