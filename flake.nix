{
  description = "hapiq: download datasets from scientific data repositories";

  inputs.nixpkgs.url = "github:NixOS/nixpkgs/nixpkgs-unstable";

  outputs = { self, nixpkgs }:
    let
      systems = [ "x86_64-linux" "aarch64-linux" "x86_64-darwin" "aarch64-darwin" ];
      forAllSystems = nixpkgs.lib.genAttrs systems;
      # A flake has no git tags, so the build is named after its commit.
      version = "devel-${self.shortRev or self.dirtyShortRev or "unknown"}";
    in
    {
      packages = forAllSystems (system:
        let pkgs = nixpkgs.legacyPackages.${system}; in rec {
          hapiq = pkgs.buildGo126Module {
            pname = "hapiq";
            inherit version;
            src = self;
            # Update after changing go.mod / go.sum: set to pkgs.lib.fakeHash,
            # run `nix build`, and copy the hash it reports.
            vendorHash = "sha256-Up6VDO7BTDatjV42G8aSmGsPAuBxcOp2InMTffHpFXc=";
            subPackages = [ "." ];
            ldflags = [ "-s" "-w" "-X github.com/btraven00/hapiq/internal/version.Version=${version}" ];
            meta = {
              description = "Download datasets from scientific data repositories";
              homepage = "https://github.com/btraven00/hapiq";
              license = pkgs.lib.licenses.gpl3Plus;
              mainProgram = "hapiq";
            };
          };
          default = hapiq;
        });
    };
}
