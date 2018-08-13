#!/usr/bin/env bash

# TODO: until https://github.com/istio/istio/issues/7745 is fixed.
# This is a temporary script, only to be used until we have a better official
# place and procedure for generation. PLEASE use with caution
# (read: not for general usage).

HUB=gcr.io/istio-testing
VERSION=$(date +%Y-%m-%d)

docker build --no-cache -t $HUB/go_generate_dependency:"$VERSION" -f Dockerfile.go_generate_dependency .

gcloud auth configure-docker

docker push $HUB/go_generate_dependency:"$VERSION"
