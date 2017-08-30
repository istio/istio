# This file should be sourced before using go commands
# it ensures that bazel's version of go is used

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MODULE="$(basename ${ROOT})"
BAZEL_DIR="${ROOT}/bazel-${MODULE}"
[[ -d ${BAZEL_DIR} ]] || { echo "Need to bazel build ... first"; exit 1; }

BDIR="$(dirname $(dirname $(readlink "${BAZEL_DIR}")))"

export GOROOT="$(find ${BDIR}/external -type d -name 'go1_*')"
export PATH=$GOROOT/bin:$PATH

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "*** Calling ${BASH_SOURCE[0]} directly has no effect. It should be sourced."
  echo "Using GOROOT: $GOROOT"
fi
