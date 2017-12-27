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

set -ex

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source ${ROOT}/../bin/docker_lib.sh

export GOPATH="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../../.." && pwd)"

cd $ROOT
export GOOS=linux
export GOARCH=amd64

echo "Prior to running this script, make sure to run 'dep ensure' to pull vendor dependencies."

CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static"' ./cmd/pilot-agent
CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static"' ./cmd/pilot-discovery
CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static"' ./cmd/sidecar-initializer
CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static"' ./test/server
CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static"' ./test/client
CGO_ENABLED=0 go build -i -ldflags '-extldflags "-static"' ./test/eurekamirror

# Collect artifacts for pushing
cp -f  client docker/client
cp -f  server docker/server
cp -f  pilot-agent docker/pilot-agent
cp -f  pilot-discovery docker/pilot-discovery
cp -f  sidecar-initializer docker/sidecar-initializer
cp -f  eurekamirror docker/eurekamirror

IMAGES=()

pushd docker
  for image in app proxy proxy_init proxy_debug pilot sidecar_initializer eurekamirror; do
    local_image="${image}:${local_tag}"
    docker build -q -f "Dockerfile.${image}" -t "${image}" .
    IMAGES+=("${image}")
  done
popd #docker

# Tag and push
tag_and_push "${IMAGES[@]}"
