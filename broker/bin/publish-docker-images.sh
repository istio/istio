#!/bin/bash

# Example usage:
#
# docker/release-docker -h docker.io/istio \
#  -t $(git rev-parse --short HEAD),$(date +%Y%m%d%H%M%S),latest"

set -ex

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

function usage() {
  echo "$0 \
    -h <docker image repository> \
    -t <comma separated list of docker image tags>"
  exit 1
}

function docker_push() {
  local im="${1}"
  if [[ "${im}" =~ ^gcr\.io ]]; then
    gcloud docker -- push ${im}
  else
    docker push ${im}
  fi
}

BAZEL_IMAGES=('broker' 'broker_debug')
IMAGES=()
TAGS=''
HUBS=''

while getopts :h:t: arg; do
  case ${arg} in
    h) HUBS="${OPTARG}";;
    t) TAGS="${OPTARG}";;
    *) usage;;
  esac
done

IFS=',' read -ra TAGS <<< "${TAGS}"
IFS=',' read -ra HUBS <<< "${HUBS}"

# Build Bazel based docker images
for IMAGE in "${BAZEL_IMAGES[@]}"; do
  bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_ARGS} "//docker:${IMAGE}"
  docker tag "istio/docker:${IMAGE}" "${IMAGE}"
  IMAGES+=("${IMAGE}")
done

# Tag and push

for IMAGE in ${IMAGES[@]}; do
  for TAG in ${TAGS[@]}; do
    for HUB in ${HUBS[@]}; do
      docker tag "${IMAGE}" "${HUB}/${IMAGE}:${TAG}"
      docker_push "${HUB}/${IMAGE}:${TAG}"
    done
  done
done
