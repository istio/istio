#!/bin/bash

set -ex

gcloud docker --authorize-only

if [ -z $DOCKER_TAG ]
then
    export DOCKER_TAG=experiment
fi

if [ -z $BAZEL_OUTBASE ]
then
    bazel run //docker:mixer
else
    bazel --output_base=$BAZEL_OUTBASE run //docker:mixer
fi

docker tag //docker:mixer gcr.io/$PROJECT/mixer:$DOCKER_TAG
docker tag gcr.io/$PROJECT/mixer:$DOCKER_TAG gcr.io/$PROJECT/mixer:latest

gcloud docker -- push gcr.io/$PROJECT/mixer:$DOCKER_TAG
gcloud docker -- push gcr.io/$PROJECT/mixer:latest
