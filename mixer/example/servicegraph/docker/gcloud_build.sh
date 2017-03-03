#!/bin/bash

set -ex

gcloud docker --authorize-only

if [ -z $DOCKER_TAG ]
then
    export DOCKER_TAG=experiment
fi

if [ -z $BAZEL_OUTBASE ]
then
    bazel run //examples/servicegraph/docker:servicegraph gcr.io/$PROJECT/servicegraph:$DOCKER_TAG
else
    bazel --output_base=$BAZEL_OUTBASE run //examples/servicegraph/docker:servicegraph gcr.io/$PROJECT/servicegraph:$DOCKER_TAG
fi

docker tag gcr.io/$PROJECT/servicegraph:$DOCKER_TAG gcr.io/$PROJECT/servicegraph:latest

gcloud docker -- push gcr.io/$PROJECT/servicegraph:$DOCKER_TAG
gcloud docker -- push gcr.io/$PROJECT/servicegraph:latest
