#!/bin/bash
#
# Copyright 2021 Istio Authors
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

# Note: Experimental only for the use of building your own on arm64 platform

set -ex

CURR_DIR=$(dirname "${BASH_SOURCE[0]}")
O_HUB=${1:-istio}

if [ ! -d ${CURR_DIR}/istio ]; then
    git clone https://github.com/istio/istio.git &&
    git clone https://github.com/istio/proxy.git &&
    git clone https://github.com/istio/tools.git
fi

# Build build-tools and build-tools-proxy
pushd ${CURR_DIR}/tools/docker/build-tools
if [ -z "$(docker images |grep "build-tools-proxy" | grep "master-latest")" ]; then
    DRY_RUN=1 time ./build-and-push.sh
fi
popd

# Build Istio binary
pushd ${CURR_DIR}/istio
TARGET_ARCH=arm64 IMAGE_VERSION=master-latest make build
popd


export IMAGE_TAG=master-latest
export IMAGE_VERSION=$IMAGE_TAG
export DOCKER_ARCHITECTURES="linux/arm64"
export TARGETARCH="arm64"
export TAG=$IMAGE_TAG
export BASE_VERSION=$IMAGE_TAG
export TARGET_ARCH=arm64
export CI=true

# Build Envoy and install envoy
export PROXY_REPO_SHA=$(cat ${CURR_DIR}/istio/istio.deps |grep "lastStableSHA" | cut -d '"' -f 4)
pushd ${CURR_DIR}/proxy
git checkout $PROXY_REPO_SHA
popd

aarch64_bin=$(file ${CURR_DIR}/istio/out/linux_arm64/release/envoy | grep aarch64) || true
if [ -z "${aarch64_bin}" ]; then
    pushd proxy
    BUILD_WITH_CONTAINER=1 make build
    BUILD_WITH_CONTAINER=1 make exportcache
    popd
    cp ${CURR_DIR}/proxy/out/linux_arm64/envoy ${CURR_DIR}/istio/out/linux_arm64/release
fi


# Build Istio base containers
cd ${CURR_DIR}/istio
DOCKER_TARGETS='docker.base docker.distroless' \
         HUBS="gcr.io/istio-release" TARGET_ARCH="arm64" make docker

# Build Istio core containers
DOCKER_TARGETS='docker.pilot docker.proxyv2 docker.app docker.pilot docker.install-cni docker.istioctl docker.operator' \
         HUBS=$O_HUB make docker
