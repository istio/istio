#!/bin/bash
set -ex

hub="gcr.io/istio-testing"
tag="test"

while getopts :h:t: arg; do
  case ${arg} in
    h) hub="${OPTARG}";;
    t) tag="${OPTARG}";;
    *) ;;
  esac
done

gcloud docker --authorize-only
for image in app init runtime; do
	bazel run //docker:$image
	docker tag istio/docker:$image $hub/$image:$tag
	docker push $hub/$image:$tag
done

bazel run //test/integration -- "$@"
