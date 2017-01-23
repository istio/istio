#!/bin/bash
set -ex
gcloud config set project istio-test

bazel run //docker:runtime
docker tag istio/docker:runtime gcr.io/istio-test/runtime:experiment
gcloud docker -- push gcr.io/istio-test/runtime:experiment
