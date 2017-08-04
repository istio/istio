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

BAZEL_IMAGES=('mixer' 'mixer_debug')
IMAGES=()
TAGS=''
HUB=''

while getopts :h:t: arg; do
  case ${arg} in
    h) HUB="${OPTARG}";;
    t) TAGS="${OPTARG}";;
    *) usage;;
  esac
done

IFS=',' read -ra TAGS <<< "${TAGS}"

if [[ "${HUB}" =~ ^gcr\.io ]]; then
  gcloud docker --authorize-only
fi

# Building grafana image
pushd "${ROOT}/deploy/kube/conf"
docker build . -t grafana
IMAGES+=(grafana)
popd

# Build Bazel based docker images
for IMAGE in "${BAZEL_IMAGES[@]}"; do
  bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_ARGS} "//docker:${IMAGE}"
  docker tag "istio/docker:${IMAGE}" "${IMAGE}"
  IMAGES+=("${IMAGE}")
done

# Tag and push

for IMAGE in "${IMAGES[@]}"; do
  for TAG in "${TAGS[@]}"; do
    if [[ -n "${HUB}"  ]]; then
      docker tag "${IMAGE}" "${HUB}/${IMAGE}:${TAG}"
      docker push "${HUB}/${IMAGE}:${TAG}"
    fi
  done
done
