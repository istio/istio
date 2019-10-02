#!/bin/bash

set -ex

istio_dir=$(git rev-parse --show-toplevel)
repo_name=$(basename $istio_dir)
target_out=$HOME/istio_out/$repo_name

image="gcr.io/istio-testing/build-tools:latest"

docker pull $image

docker run -it --rm -u $(id -u) \
    -u root \
    --cap-add=NET_ADMIN \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /etc/passwd:/etc/passwd:ro \
    $CONTAINER_OPTIONS \
    -e WHAT=$WHAT \
    --mount type=bind,source="$istio_dir",destination="/work" \
    --mount type=bind,source="$target_out",destination="/targetout" \
    --mount type=volume,source=home,destination="/home" \
    -w /work $image $@

