#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
################################################################################
#
# This script creates a base image for the init image that includes iptables.
# Prior to running this script, make sure to run 'dep ensure' to pull vendor dependencies

set -o errexit
set -o nounset
set -o pipefail
set -x

hub="gcr.io/istio-testing"
tag=$(whoami)_$(date +%y%m%d_%H%M%S)

while [[ $# -gt 0 ]]; do
    case "$1" in
        -tag) tag="$2"; shift ;;
        -hub) hub="$2"; shift ;;
        *) ;;
    esac
    shift
done

export GOOS=linux
export GOARCH=amd64

echo "Prior to running this script, make sure to run 'dep ensure' to pull vendor dependencies."

go build ./cmd/pilot-agent
go build ./cmd/pilot-discovery
go build ./cmd/sidecar-initializer
go build ./test/server
go build ./test/client
go build ./test/eurekamirror

# Collect artifacts for pushing
cp -f  client docker/client
cp -f  server docker/server
cp -f  pilot-agent docker/pilot-agent
cp -f  pilot-discovery docker/pilot-discovery
cp -f  sidecar-initializer docker/sidecar-initializer
cp -f  eurekamirror docker/eurekamirror

# Build and push images
if [[ "$hub" =~ ^gcr\.io ]]; then
  gcloud docker --authorize-only
fi

pushd docker
  for image in app proxy proxy_init proxy_debug pilot sidecar_initializer eurekamirror; do
    docker build -f "Dockerfile.${image}" -t "$hub/$image:$tag" .
    docker push "$hub/$image:$tag"
  done
popd

echo Pushed images to $hub with tag $tag
