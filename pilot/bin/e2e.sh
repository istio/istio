#!/bin/bash

# This script is a workaround due to inability to invoke bazel run targets from within bazel sandboxes.
# It is a simple shim over test/integration/driver.go that accepts the same set of flags.
# Please add new flags to the Go test driver directly instead of extending this file.
# The additional steps that the script performs are:
# - set default docker tag based on a timestamp and user name
# - build and push docker images, including manager pieces and proxy.

args=""
hub="gcr.io/istio-testing"
tag=""
debug_suffix=""

while [[ $# -gt 0 ]]; do
    case "$1" in
        -h) hub="$2"; shift ;;
        -t) tag="$2"; shift ;;
        --use_debug_image) debug_suffix="_debug" ;;
        *) args=$args" $1" ;;
    esac
    shift
done

set -ex

if [[ -z $tag ]]; then
  tag=$(whoami)_$(date +%Y%m%d_%H%M%S)
fi
args=$args" -t $tag"

if [[ "$hub" =~ ^gcr\.io ]]; then
  gcloud docker --authorize-only
fi
args=$args" -h $hub"

for image in app init runtime; do
  bazel $BAZEL_ARGS run //docker:$image$debug_suffix
  docker tag istio/docker:$image$debug_suffix $hub/$image:$tag
  docker push $hub/$image:$tag
done

bazel $BAZEL_ARGS run //test/integration -- $args
