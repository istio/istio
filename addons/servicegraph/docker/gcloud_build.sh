#!/bin/bash

set -ex

gcloud docker --authorize-only

if [ -z $DOCKER_TAG ]
then
    export DOCKER_TAG=experiment
fi

if [ -z $BAZEL_OUTBASE ]
then
    bazel run //example/servicegraph/docker:servicegraph 
    docker tag istio/example/servicegraph/docker:servicegraph gcr.io/$PROJECT/servicegraph:$DOCKER_TAG
else
    bazel --output_base=$BAZEL_OUTBASE run //example/servicegraph/docker:servicegraph
    docker tag istio/example/servicegraph/docker:servicegraph 
fi

docker tag gcr.io/$PROJECT/servicegraph:$DOCKER_TAG gcr.io/$PROJECT/servicegraph:latest

gcloud docker -- push gcr.io/$PROJECT/servicegraph:$DOCKER_TAG
gcloud docker -- push gcr.io/$PROJECT/servicegraph:latest
