{
  description = "OpenClaw Dashboard — real-time bot monitoring UI";

  inputs = {
    nixpkgs.url = "github:NixOS/nixpkgs/nixos-unstable";
    flake-utils.url = "github:numtide/flake-utils";
  };

  outputs = { self, nixpkgs, flake-utils }:
    flake-utils.lib.eachDefaultSystem (system:
      let
        pkgs = nixpkgs.legacyPackages.${system};
        python = pkgs.python312;
        runtimeDeps = [ pkgs.bash pkgs.curl pkgs.jq ];
      in {
        packages.default = pkgs.stdenv.mkDerivation {
          pname = "openclaw-dashboard";
          version = "0.1.0";
          src = ./.;

          nativeBuildInputs = [ pkgs.makeWrapper ];
          buildInputs = [ python ] ++ runtimeDeps;

          installPhase = ''
            mkdir -p $out/share/openclaw-dashboard $out/bin
            cp index.html server.py refresh.sh themes.json $out/share/openclaw-dashboard/
            chmod +x $out/share/openclaw-dashboard/refresh.sh
            makeWrapper ${python}/bin/python3 $out/bin/openclaw-dashboard \
              --add-flags "$out/share/openclaw-dashboard/server.py" \
              --prefix PATH : ${pkgs.lib.makeBinPath runtimeDeps}
          '';

          meta = {
            description = "OpenClaw real-time bot monitoring dashboard";
            license = pkgs.lib.licenses.mit;
            platforms = pkgs.lib.platforms.unix;
          };
        };

        devShells.default = pkgs.mkShell {
          buildInputs = [ python pkgs.bash pkgs.curl pkgs.jq ];
          shellHook = ''
            echo "OpenClaw Dashboard dev shell"
            echo "Run:  python3 server.py"
            echo "Test: python3 -m pytest tests/ -v"
          '';
        };

        apps.default = flake-utils.lib.mkApp {
          drv = self.packages.${system}.default;
          exePath = "/bin/openclaw-dashboard";
        };
      });
}
