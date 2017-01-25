#!/bin/bash

set -ex

gcloud config set project istio-test

bazel run //docker:mixer gcr.io/istio-test/mixer:experiment

gcloud docker -- push gcr.io/istio-test/mixer:experiment
