# This file should be sourced before using go commands
# it ensures that bazel's version of go is used

SP=$( cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P )
BDIR=$(dirname $(dirname $(readlink $SP/../bazel-mixer )))

export GOROOT=$(ls -1d $BDIR/external/golang_*)
export PATH=$GOROOT/bin:$PATH

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "*** Calling ${BASH_SOURCE[0]} directly has no effect. It should be sourced."
  echo "Using GOROOT: $GOROOT" 
  go version
fi
