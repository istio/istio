#!/bin/bash

# Example usage:
#
# docker/release-docker -h docker.io/istio \
#  -t $(git rev-parse --short HEAD),$(date +%Y%m%d%H%M%S),latest"

set -ex

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"


source ${ROOT}/../bin/docker_lib.sh

BAZEL_IMAGES=('mixer' 'mixer_debug')
IMAGES=()

# Building grafana image
pushd "${ROOT}/deploy/kube/conf"
docker build . -t grafana
IMAGES+=(grafana)
popd

BAZEL_STARTUP_ARGS=${BAZEL_STARTUP_ARGS:-}
BAZEL_ARGS=${BAZEL_ARGS:-}

# Build Bazel based docker images
for IMAGE in "${BAZEL_IMAGES[@]}"; do
  bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_ARGS} "//mixer/docker:${IMAGE}"
  docker tag "istio/mixer/docker:${IMAGE}" "${IMAGE}"
  IMAGES+=("${IMAGE}")
done

# Build Servicegraph
bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_ARGS} "//mixer/example/servicegraph/docker:servicegraph"
docker tag "istio/mixer/example/servicegraph/docker:servicegraph" "servicegraph"
IMAGES+=(servicegraph)

# Tag and push

tag_and_push "${IMAGES[@]}"
