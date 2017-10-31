# This file should be sourced before using go commands
# it ensures that bazel's version of go is used

SP=$( cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P )

TARGETBIN=$SP/../bazel-broker
if [[ ! -e $TARGETBIN ]]; then
  echo "*** $TARGETBIN does not exist - did you forget to bazel build ... ?"
  exit 1
fi

BDIR=$(dirname $(dirname $(readlink $TARGETBIN)))

export GOROOT="$(find ${BDIR}/external -type d -name 'go1_*')"
export PATH=$GOROOT/bin:$PATH

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "*** Calling ${BASH_SOURCE[0]} directly has no effect. It should be sourced."
  echo "Using GOROOT: $GOROOT"
  go version
fi