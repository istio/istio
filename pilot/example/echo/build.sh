#!/bin/bash
gcloud config set project istio-test

# Expects "mixs", "envoy_esp", "cmd" binaries in the directory
for s in mixer manager envoy; do
  docker build -t $s -f Dockerfile-$s .
  docker tag $s gcr.io/istio-test/$s:example
  gcloud docker -- push gcr.io/istio-test/$s:example
done
