#!/bin/bash

set -ex

hub=gcr.io/istio-testing
tag=ubuntu_xenial_debug
filename=ubuntu_xenial_debug

docker build . -t $hub:$tag
docker save $hub:$tag | gzip > $filename.tar.gz
gsutil cp $filename.tar.gz gs://istio-build/manager/$filename.tar.gz
sum=$(sha256sum $filename.tar.gz)

echo "Update the sha256 to $sum for the $filename http_file() rule in WORKSPACE"
