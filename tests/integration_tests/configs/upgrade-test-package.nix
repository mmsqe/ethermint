let
  pkgs = import ../../../nix { };
  fetchFlake = repo: rev: (pkgs.flake-compat {
    src = {
      outPath = builtins.fetchTarball "https://github.com/${repo}/archive/${rev}.tar.gz";
      inherit rev;
      shortRev = builtins.substring 0 7 rev;
    };
  }).defaultNix;
  released = (fetchFlake "crypto-org-chain/ethermint" "b216a320ac6a60b019c1cbe5a6b730856482f071").default;
  sdk50 = (fetchFlake "crypto-org-chain/ethermint" "21bd7ce300e825f5b58639920df142a84f4c8637").default;
  current = pkgs.callPackage ../../../. { };
in
pkgs.linkFarm "upgrade-test-package" [
  { name = "genesis"; path = released; }
  { name = "sdk50"; path = sdk50; }
  { name = "sdk52"; path = current; }
]
