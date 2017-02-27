#!/bin/bash

set -ex

hub=gcr.io/istio-testing
tag=ubuntu_xenial_debug
name=ubuntu_xenial_debug

docker build . -t $hub:$tag
docker save $hub:$tag | gzip > $name.tar.gz
sum=$(sha256sum $name.tar.gz | cut -f1 -d' ')
gsutil cp $name.tar.gz gs://istio-build/manager/$name-$sum.tar.gz

echo "Update DEBUG_BASE_IMAGE_SHA to $sum in WORKSPACE"
