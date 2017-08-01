#!/bin/bash

# Example usage:
#
# docker/release-docker -h docker.io/istio \
#  -c release \
#  -t $(git rev-parse --short HEAD),$(date +%Y%m%d%H%M%S),latest" \
#  -i "init,init_debug,app,app_debug,runtime,runtime_debug"

function usage() {
  echo "$0 \
    -c <bazel config to use> \
    -h <docker image repository> \
    -t <comma separated list of docker image tags> \
    -i <comma separated list of docker images>"
  exit 1
}

IMAGES='istio-ca'

while getopts :c:h:i:t:: arg; do
  case ${arg} in
    c) BAZEL_ARGS="--config=${OPTARG}";;
    h) HUB="${OPTARG}";;
    i) IMAGES="${OPTARG}";;
    t) TAGS="${OPTARG}";;
  esac
done

[[ -z "${HUB}" ]] && usage
[[ -z "${TAGS}" ]] && usage
[[ -z "${IMAGES}" ]] && usage

IFS=',' read -ra TAGS <<< "${TAGS}"
IFS=',' read -ra IMAGES <<< "${IMAGES}"

if [[ "${HUB}" =~ ^gcr\.io ]]; then
  gcloud docker --authorize-only
fi

set -ex

for IMAGE in "${IMAGES[@]}"; do
  bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_ARGS} "//docker:${IMAGE}"
  for TAG in "${TAGS[@]}"; do
    docker tag bazel/docker:"${IMAGE}" "${HUB}/${IMAGE}:${TAG}"
    docker push "${HUB}/${IMAGE}:${TAG}"
  done
done
