#!/bin/bash

set -ex

PROJECT=$(gcloud config get-value project)

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

docker tag istio/docker:mixer gcr.io/$PROJECT/mixer:$DOCKER_TAG
gcloud docker -- push gcr.io/$PROJECT/mixer:$DOCKER_TAG
