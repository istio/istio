# This file should be sourced before using go commands
# it ensures that bazel's version of go is used

BAZEL_DIR="$(bazel info execution_root)"

export GOROOT="$(find ${BAZEL_DIR}/external -type d -name 'go1_*')"
export PATH=$GOROOT/bin:$PATH

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "*** Calling ${BASH_SOURCE[0]} directly has no effect. It should be sourced."
  echo "Using GOROOT: $GOROOT"
fi
