#!/bin/bash
gcloud config set project istio-test

rm -f cmd
bazel build //...
cp ../../../bazel-bin/cmd/cmd .

docker build -t runtime -f Dockerfile .
docker tag runtime gcr.io/istio-test/runtime:test
gcloud docker -- push gcr.io/istio-test/runtime:test
