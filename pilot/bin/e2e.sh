#!/bin/bash

hub="gcr.io/istio-testing"
namespace=""
debug_suffix=""

# manager is known to work with this mixer build
# update manually
mixerTag="ea3a8d3e2feb9f06256f92cda5194cc1ea6b599e"

args=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        -h) hub="$2"; shift ;;
        -t) tag="$2"; shift ;;
        --mixerTag) mixerTag="$2"; shift ;;
        --skipBuild) SKIP_BUILD=1 ;;
        -n) namespace="$2"; shift ;;
        --use_debug_image) debug_suffix="_debug" ;;
        *) args=$args" $1" ;;
    esac
    shift
done

set -ex


if [[ -z $SKIP_BUILD ]];then
	if [[ -z $tag ]];then
		# generate tag if performing a build.
		tag=$(whoami)_$(date +%Y%m%d_%H%M%S)
	fi
	if [[ "$hub" =~ ^gcr\.io ]]; then
		gcloud docker --authorize-only
	fi
	for image in app init runtime; do
		bazel $BAZEL_ARGS run //docker:$image$debug_suffix
		docker tag istio/docker:$image$debug_suffix $hub/$image:$tag
		docker push $hub/$image:$tag
	done
fi

[[ ! -z "$tag" ]]       && args=$args" -t $tag"
[[ ! -z "$hub" ]]       && args=$args" -h $hub"
[[ ! -z "$namespace" ]] && args=$args" -n $namespace"

# mixerTag will never be empty
args=$args" --mixerTag $mixerTag"


bazel $BAZEL_ARGS run //test/integration -- $args --norouting
