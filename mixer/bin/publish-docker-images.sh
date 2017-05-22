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

IMAGES=()
TAGS=''
HUB=''

while getopts :h:t:: arg; do
  case ${arg} in
    h) HUB="${OPTARG}";;
    t) TAGS="${OPTARG}";;
  esac
done

[[ -z "${HUB}" ]] && usage
[[ -z "${TAGS}" ]] && usage

IFS=',' read -ra TAGS <<< "${TAGS}"

if [[ "${HUB}" =~ ^gcr\.io ]]; then
  gcloud docker --authorize-only
fi

# Building grafana image
pushd "${ROOT}/deploy/kube/conf"
docker build . -t grafana
IMAGES+=(grafana)
popd

for IMAGE in "${IMAGES[@]}"; do
  for TAG in "${TAGS[@]}"; do
    docker tag "${IMAGE}" "${HUB}/${IMAGE}:${TAG}"
    docker push "${HUB}/${IMAGE}:${TAG}"
  done
done
