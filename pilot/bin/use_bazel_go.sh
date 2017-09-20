# This file should be sourced before using go commands
# it ensures that bazel's version of go is used

EXEC_ROOT="$(bazel info execution_root)"

if [[ ! -e ${EXEC_ROOT} ]]; then
  echo "*** ${EXEC_ROOT} does not exist - did you forget to bazel build ... ?"
  exit 1
fi

export GOROOT="$(find ${EXEC_ROOT}/external -type d -name 'go1_*')"
export PATH=${GOROOT}/bin:${PATH}

if [[ "${BASH_SOURCE[0]}" == "${0}" ]]; then
  echo "*** Calling ${BASH_SOURCE[0]} directly has no effect. It should be sourced."
  echo "Using GOROOT: ${GOROOT}"
  go version
fi
