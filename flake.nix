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
        runtimeDeps = [ pkgs.bash pkgs.curl pkgs.jq pkgs.git ];
      in {
        packages = {
          # Go binary (default) — single binary, zero runtime deps
          default = pkgs.buildGoModule {
            pname = "openclaw-dashboard";
            version = "2026.3.8";
            src = ./.;
            vendorHash = null; # no external deps

            ldflags = [ "-s" "-w" ];

            nativeBuildInputs = [ pkgs.makeWrapper ];

            postInstall = ''
              mkdir -p $out/share/openclaw-dashboard/examples
              cp ${./refresh.sh} $out/share/openclaw-dashboard/refresh.sh
              cp ${./themes.json} $out/share/openclaw-dashboard/themes.json
              cp ${./config.json} $out/share/openclaw-dashboard/config.json
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
              platforms = pkgs.lib.platforms.unix;
              mainProgram = "openclaw-dashboard";
            };
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = [
            pkgs.go
            pkgs.bash pkgs.curl pkgs.jq pkgs.git
          ];
          shellHook = ''
            echo "OpenClaw Dashboard dev shell"
            echo ""
            echo "  Go:     go run . --port 8080"
            echo "  Build:  go build -ldflags='-s -w' -o openclaw-dashboard ."
            echo "  Test:   go test -race -v -count=1 ./..."
          '';
        };

        apps = {
          default = flake-utils.lib.mkApp {
            drv = self.packages.${system}.default;
            exePath = "/bin/openclaw-dashboard";
          };
        };
      });
}
