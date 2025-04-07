{ pkgs
, config
, ethermint ? (import ../. { inherit pkgs; })
}: rec {
  start-ethermint = pkgs.writeShellScriptBin "start-ethermint" ''
    # rely on environment to provide ethermintd
    export PATH=${pkgs.test-env}/bin:$PATH
    ${../scripts/start-ethermint.sh} ${config.ethermint-config} ${config.dotenv} $@
  '';
  start-geth = pkgs.writeShellScriptBin "start-geth" ''
    export PATH=${pkgs.test-env}/bin:${pkgs.go-ethereum}/bin:$PATH
    source ${config.dotenv}
    ${../scripts/start-geth.sh} ${config.geth-genesis} $@
  '';
  start-beacon = pkgs.writeShellScriptBin "start-beacon" ''
    export USE_PRYSM_VERSION=v5.0.4
    ${../scripts/start-beacon.sh} $@
  '';
  start-validator = pkgs.writeShellScriptBin "start-validator" ''
    export USE_PRYSM_VERSION=v5.0.4
    ${../scripts/start-validator.sh} $@
  '';
  start-scripts = pkgs.symlinkJoin {
    name = "start-scripts";
    paths = [ start-ethermint start-geth start-beacon start-validator ];
  };
}
