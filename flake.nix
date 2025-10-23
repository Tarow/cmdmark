{
  description = "cmdmark";

  inputs = {
    nixpkgs.url = "github:nixos/nixpkgs/nixos-25.05";
  };
  outputs = {
    self,
    nixpkgs,
    ...
  }: let
    systems = [
      "aarch64-linux"
      "i686-linux"
      "x86_64-linux"
      "aarch64-darwin"
      "x86_64-darwin"
    ];
    forAllSystems = nixpkgs.lib.genAttrs systems;
  in
    {
      devShells = forAllSystems (system: let
        pkgs = nixpkgs.legacyPackages.${system};
      in rec {
        default = dockdns;
        dockdns = pkgs.mkShell {
          packages = with pkgs; [
            go
            golangci-lint
            air
            gopls
            gotools
            delve
          ];
        };
      });

      packages =
        forAllSystems
        (system: let
          pkgs = nixpkgs.legacyPackages.${system};
          lib = nixpkgs.lib;
        in rec {
          default = cmdmark;

          cmdmark = pkgs.buildGoModule {
            name = "cmdmark";
            pname = "cmdmark";
            version = toString (self.shortRev or self.dirtyShortRev or self.lastModified or "unknown");
            buildInputs = nixpkgs.lib.lists.optionals pkgs.stdenv.isDarwin [pkgs.darwin.apple_sdk.frameworks.AppKit];
            src = lib.fileset.toSource {
              root = ./.;
              fileset = lib.fileset.unions [
                ./go.mod
                ./go.sum
                ./main.go
                ./config.go
              ];
            };
            vendorHash = "sha256-yyogHNuGogtJcsSvylp6NrL2ffjwW+OCD5gc0ajBG+c=";
            meta.mainProgram = "cmdmark";
          };
        });
    }
    // (let
      mkModule = optPath: ({
        config,
        pkgs,
        lib,
        ...
      }: let
        cfg = config.programs.cmdmark;
        yaml = pkgs.formats.yaml {};
        cmdMark = self.packages.${pkgs.stdenv.hostPlatform.system}.cmdmark;
        wrappedCmdMark = pkgs.symlinkJoin {
          name = cmdMark.name;
          paths = [cmdMark];
          buildInputs = [pkgs.makeWrapper];
          postBuild = "wrapProgram $out/bin/cmdmark --add-flags ${cfg.settings}";
        };
      in {
        options.programs.cmdmark = {
          enable = lib.mkEnableOption "cmdmark";
          settings = lib.mkOption {
            type = yaml.type;
            apply = yaml.generate "config.yml";
            description = "YAML configuration for cmdmark";
          };
        };
        config = lib.mkIf cfg.enable (lib.setAttrByPath optPath [wrappedCmdMark]);
      });
    in {
      nixosModules.cmdmark = mkModule ["environment" "systemPackages"];
      homeModules.cmdmark = mkModule ["home" "packages"];
    });
}
