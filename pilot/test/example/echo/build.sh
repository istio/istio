#!/bin/bash
gcloud config set project istio-test

rm -f cmd
bazel build //...
cp ../../bazel-bin/cmd/cmd .

# Expects "mixs", "envoy_esp", "cmd" binaries in the directory
for s in mixer manager envoy; do
  docker build -t $s -f Dockerfile-$s .
  docker tag $s gcr.io/istio-test/$s:test
  gcloud docker -- push gcr.io/istio-test/$s:test
done
