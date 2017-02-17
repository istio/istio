#!/bin/bash

hub="gcr.io/istio-testing"
tag=$(whoami)_$(date +%Y%m%d_%H%M%S)
namespace=""
debug_suffix=""

args=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -h) hub="$2"; shift ;;
        -t) tag="$2"; shift ;;
        -n) namespace="$2"; shift ;;
        --use_debug_image) debug_suffix="_debug" ;;
        *) args=$args" $1" ;;
    esac
    shift
done

[[ ! -z "$tag" ]]       && args=$args" -t $tag"
[[ ! -z "$hub" ]]       && args=$args" -h $hub"
[[ ! -z "$namespace" ]] && args=$args" -n $namespace"

set -ex

if [[ "$hub" =~ ^gcr\.io ]]; then
    gcloud docker --authorize-only
fi

for image in app init runtime; do
    bazel run //docker:$image$debug_suffix
    docker tag istio/docker:$image$debug_suffix $hub/$image:$tag
    docker push $hub/$image:$tag
done

bazel run //test/integration -- $args --norouting
