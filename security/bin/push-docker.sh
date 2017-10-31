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

function docker_push() {
  local im="${1}"
  if [[ "${im}" =~ ^gcr\.io ]]; then
    gcloud docker -- push "${im}"
  else
    docker push "${im}"
  fi
}

IMAGES='istio-ca'
HUBS=''

while getopts :c:h:i:t:: arg; do
  case ${arg} in
    c) BAZEL_ARGS="--config=${OPTARG}";;
    h) HUBS="${OPTARG}";;
    i) IMAGES="${OPTARG}";;
    t) TAGS="${OPTARG}";;
  esac
done

[[ -z "${HUBS}" ]] && usage
[[ -z "${TAGS}" ]] && usage
[[ -z "${IMAGES}" ]] && usage

IFS=',' read -ra TAGS <<< "${TAGS}"
IFS=',' read -ra IMAGES <<< "${IMAGES}"
IFS=',' read -ra HUBS <<< "${HUBS}"


set -ex

for IMAGE in "${IMAGES[@]}"; do
  bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_ARGS} "//docker:${IMAGE}"
  for TAG in "${TAGS[@]}"; do
    for HUB in "${HUBS[@]}"; do
      docker tag bazel/docker:"${IMAGE}" "${HUB}/${IMAGE}:${TAG}"
      docker_push "${HUB}/${IMAGE}:${TAG}"
    done
  done
done
