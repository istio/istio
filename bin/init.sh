#!/bin/bash
#
# Copyright 2017,2018 Istio Authors. All Rights Reserved.
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

# Init script downloads or updates envoy and the go dependencies. Called from Makefile, which sets
# the needed environment variables.

ROOT=$(cd $(dirname $0)/..; pwd)
ISTIO_GO=$ROOT

set -o errexit
set -o nounset
set -o pipefail
set -x # echo on

# Set GOPATH to match the expected layout
GO_TOP=$(cd $(dirname $0)/../../../..; pwd)
OUT=${GO_TOP}/out

export GOPATH=${GOPATH:-$GO_TOP}
# Normally set by Makefile
export ISTIO_BIN=${ISTIO_BIN:-${GOPATH}/bin}

# test scripts seem to like to run this script directly rather than use make
export ISTIO_OUT=${ISTIO_OUT:-${ISTIO_BIN}}

# Ensure expected GOPATH setup
if [ ${ROOT} != "${GO_TOP:-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

# Get the debug istio.io/proxy docker image. We'll use this image to extract the envoy executable used by
# some of our tests.
PROXY_DOCKERFILE="pilot/docker/Dockerfile.proxy_debug"
ENVOY_IMAGE=$(grep envoy-debug $PROXY_DOCKERFILE  |cut -d ' ' -f2)
ENVOY_IMAGE_VERSION=$(grep envoy-debug $PROXY_DOCKERFILE  |cut -d: -f2)
ENVOY_BIN_NAME=envoy-$ENVOY_IMAGE_VERSION

if [ ! -f $OUT/$ENVOY_BIN_NAME ] ; then
    mkdir -p $OUT
    pushd $OUT

    # Pull the image to the local docker registry.
    echo "Downloading envoy docker image..."
    docker pull $ENVOY_IMAGE

    # Run the image and pull out the envoy executable
    echo "Extracting envoy from docker image..."
    docker run $ENVOY_IMAGE cat /usr/local/bin/envoy > $ENVOY_BIN_NAME

    # Set executable permissions on the binary.
    chmod +x $ENVOY_BIN_NAME

    # Remove any old envoy binaries.
    rm -f ${ISTIO_OUT}/envoy ${ROOT}/pilot/pkg/proxy/envoy/envoy ${ISTIO_BIN}/envoy
    popd
fi

if [ ! -f ${ISTIO_OUT}/envoy ] ; then
    mkdir -p ${ISTIO_OUT}
    # Make sure the envoy binary exists.
    cp $OUT/$ENVOY_BIN_NAME ${ISTIO_OUT}/envoy
fi

# circleCI expects this in the bin directory
if [ ! -f ${ISTIO_BIN}/envoy ] ; then
    mkdir -p ${ISTIO_BIN}
    cp $OUT/$ENVOY_BIN_NAME ${ISTIO_BIN}/envoy
fi
