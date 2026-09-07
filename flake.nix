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
        # Project requires Go 1.27+ (see go.mod). Pin explicitly so we don't
        # silently drift to whatever the nixpkgs channel default is.
        go = pkgs.go_1_27;
        runtimeDeps = [ pkgs.bash pkgs.git ] ++ pkgs.lib.optionals pkgs.stdenv.isLinux [ pkgs.procps ];
        version = pkgs.lib.fileContents ./VERSION;
      in {
        packages = {
          # Go binary with packaged runtime tools and data
          default = (pkgs.buildGoModule.override { inherit go; }) {
            pname = "openclaw-dashboard";
            inherit version;
            src = ./.;
            vendorHash = null; # no external deps
            subPackages = [ "cmd/openclaw-dashboard" ];

            env.CGO_ENABLED = "0";
            flags = [ "-trimpath" ];
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
                --prefix PATH : ${pkgs.lib.makeBinPath runtimeDeps} \
                --set-default ZONEINFO ${pkgs.tzdata}/share/zoneinfo
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
            pkgs.nodejs # required by `make frontend-test`
          ];
          shellHook = ''
            echo "OpenClaw Dashboard dev shell"
            echo ""
            echo "  Go:     go run ./cmd/openclaw-dashboard --port 8080"
            echo "  Build:  make build"
            echo "  Test:   make test"
            echo "  Check:  make check"
            echo "  Vuln:   make govulncheck"
            echo "  Lint:   make lint"
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
          runtime = pkgs.runCommand "openclaw-dashboard-runtime-check" {
            nativeBuildInputs = [ self.packages.${system}.default pkgs.jq pkgs.coreutils ];
          } ''
            export HOME="$TMPDIR/home"
            export OPENCLAW_STATE_DIR="$HOME/.openclaw"
            mkdir -p "$OPENCLAW_STATE_DIR/dashboard"
            echo '{}' > "$OPENCLAW_STATE_DIR/openclaw.json"
            cat > "$OPENCLAW_STATE_DIR/dashboard/config.json" <<'EOF'
            {"timezone":"Asia/Kuala_Lumpur","openclaw":{"mode":"native","binary":"/missing-nix-fixture"},"ai":{"enabled":false},"system":{"enabled":false}}
            EOF
            test "$(openclaw-dashboard --version)" = 'openclaw-dashboard ${pkgs.lib.removePrefix "v" version}'
            openclaw-dashboard --refresh
            runtime="$OPENCLAW_STATE_DIR/dashboard"
            jq -e '.timezone == "Asia/Kuala_Lumpur"' "$runtime/data.json"
            test "$(stat -c %a "$runtime/data.json")" = 600
            test -s "$runtime/themes.json"
            test -x "$runtime/refresh.sh"
            test -s "$runtime/examples/config.minimal.json"
            touch "$out"
          '';
        };
      });
}
