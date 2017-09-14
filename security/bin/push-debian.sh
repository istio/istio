#!/bin/bash

# Example usage:
#
# bin/push-debian.sh \
#   -c opt
#   -v 0.2.1
#   -p gs://istio-release/release/0.2.1/deb

function usage() {
  echo "$0 \
    -c <bazel config to use> \
    -p <GCS path, e.g. gs://istio-release/release/0.2.1/deb> \
    -v <istio version number>"
  exit 1
}

while getopts ":c:p:v:" arg; do
  case ${arg} in
    c) BAZEL_ARGS="--config=${OPTARG}";;
    p) GCS_PATH="${OPTARG}";;
    v) ISTIO_VERSION="${OPTARG}";;
    *) usage;;
  esac
done

if [ -z "${BAZEL_ARGS}" ] || [ -z "${ISTIO_VERSION}" ] || [ -z "${GCS_PATH}" ]; then
  usage
fi

set -ex

bazel ${BAZEL_STARTUP_ARGS} build ${BAZEL_ARGS} "//tools/deb:istio-auth-node-agent"
gsutil -m cp -r \
  bazel-bin/tools/deb/istio-auth-node-agent_${ISTIO_VERSION}_amd64.* \
  ${GCS_PATH}
