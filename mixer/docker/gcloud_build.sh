#!/bin/bash

set -ex

gcloud docker --authorize-only

bazel run //docker:mixer gcr.io/$PROJECT/mixer:experiment

gcloud docker -- push gcr.io/$PROJECT/mixer:experiment
