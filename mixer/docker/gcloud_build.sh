#!/bin/bash

set -ex

gcloud docker --authorize-only

if [ -z $BAZEL_OUTBASE ]
then
    bazel run //docker:mixer gcr.io/$PROJECT/mixer:experiment
else
    bazel --output_base=$BAZEL_OUTBASE run //docker:mixer gcr.io/$PROJECT/mixer:experiment
fi

gcloud docker -- push gcr.io/$PROJECT/mixer:experiment
