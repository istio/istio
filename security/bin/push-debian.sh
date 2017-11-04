#!/bin/bash

# Example usage:
#
# bin/push-debian.sh \
#   -c opt
#   -v 0.2.1
#   -p gs://istio-release/release/0.2.1/deb

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
VERSION_FILE="${ROOT}/tools/deb/version"
BAZEL_ARGS=()
BAZEL_TARGET='//security/tools/deb:istio-auth-node-agent'
BAZEL_BINARY="${ROOT}/../bazel-bin/security/tools/deb/istio-auth-node-agent"

set -ex

function usage() {
  echo "$0 \
    -c <bazel config to use> \
    -p <GCS path, e.g. gs://istio-release/release/0.2.1/deb> \
    -v <istio version number>"
  exit 1
}

while getopts ":c:p:v:" arg; do
  case ${arg} in
    c) BAZEL_ARGS+=("--config=${OPTARG}");;
    p) GCS_PATH="${OPTARG}";;
    v) ISTIO_VERSION="${OPTARG}";;
    *) usage;;
  esac
done

if [[ -n "${ISTIO_VERSION}" ]]; then
  BAZEL_TARGET+='-release'
  BAZEL_BINARY+='-release'
  echo "${ISTIO_VERSION}" > "${VERSION_FILE}"
  trap 'rm "${VERSION_FILE}"' EXIT
fi

[[ -z "${GCS_PATH}" ]] && usage

bazel build ${BAZEL_ARGS[@]} ${BAZEL_TARGET}

gsutil -m cp -r "${BAZEL_BINARY}.deb" ${GCS_PATH}/
