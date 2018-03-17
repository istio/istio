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

# TODO(nmittler): Remove before merging.
set -x # echo on

# TODO(nmittler): Remove these variables and require that this script be run from the Makefile

# Set GOPATH to match the expected layout
GO_TOP=$(cd $(dirname $0)/../../../..; pwd)

export OUT_DIR=${OUT_DIR:-${GO_TOP}/out}

HELM_VER=v2.7.2

export GOPATH=${GOPATH:-$GO_TOP}
# Normally set by Makefile
export ISTIO_BIN=${ISTIO_BIN:-${GOPATH}/bin}

# Set the architecture. Matches logic in the Makefile.
export GOARCH=${GOARCH:-'amd64'}

# Determine the OS. Matches logic in the Makefile.
LOCAL_OS="`uname`"
case $LOCAL_OS in
  'Linux')
    LOCAL_OS='linux'
    ;;
  'Darwin')
    LOCAL_OS='darwin'
    ;;
  *)
    echo "This system's OS ${LOCAL_OS} isn't recognized/supported"
    exit 1
    ;;
esac
export GOOS=${GOOS:-${LOCAL_OS}}

# test scripts seem to like to run this script directly rather than use make
export ISTIO_OUT=${ISTIO_OUT:-${ISTIO_BIN}}

# Ensure expected GOPATH setup
if [ ${ROOT} != "${GO_TOP:-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

# Download Envoy debug and release binaries for Linux x86_64. They will be included in the
# docker images created by Dockerfile.proxy and Dockerfile.proxy_debug.

# Gets the download command supported by the system (currently either curl or wget)
DOWNLOAD_COMMAND=""
set_download_command () {
    # Try curl.
    if command -v curl > /dev/null; then
        if curl --version | grep Protocols  | grep https > /dev/null; then
	       DOWNLOAD_COMMAND='curl -fLSs'
	       return
        fi
        echo curl does not support https, will try wget for downloading files.
    else
        echo curl is not installed, will try wget for downloading files.
    fi

    # Try wget.
    if command -v wget > /dev/null; then
        DOWNLOAD_COMMAND='wget -qO -'
        return
    fi
    echo wget is not installed.

    echo Error: curl is not installed or does not support https, wget is not installed. \
         Cannot download envoy. Please install wget or add support of https to curl.
    exit 1
}

if [ -z ${PROXY_REPO_SHA:-} ] ; then
  export PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
fi

# Normally set by the Makefile.
ISTIO_ENVOY_VERSION=${ISTIO_ENVOY_VERSION:-${PROXY_REPO_SHA}}
ISTIO_ENVOY_DEBUG_URL=${ISTIO_ENVOY_DEBUG_URL:-https://storage.googleapis.com/istio-build/proxy/envoy-debug-${ISTIO_ENVOY_VERSION}.tar.gz}
ISTIO_ENVOY_RELEASE_URL=${ISTIO_ENVOY_RELEASE_URL:-https://storage.googleapis.com/istio-build/proxy/envoy-alpha-${ISTIO_ENVOY_VERSION}.tar.gz}

# Normally set by the Makefile.
# Variables for the extracted debug/release Envoy artifacts.
ISTIO_ENVOY_DEBUG_DIR=${ISTIO_ENVOY_DEBUG_DIR:-"${OUT_DIR}/${GOOS}_${GOARCH}/debug"}
ISTIO_ENVOY_DEBUG_NAME=${ISTIO_ENVOY_DEBUG_NAME:-"envoy-debug-$ISTIO_ENVOY_VERSION"}
ISTIO_ENVOY_DEBUG_PATH=${ISTIO_ENVOY_DEBUG_PATH:-"$ISTIO_ENVOY_DEBUG_DIR/$ISTIO_ENVOY_DEBUG_NAME"}
ISTIO_ENVOY_RELEASE_DIR=${ISTIO_ENVOY_RELEASE_DIR:-"${OUT_DIR}/${GOOS}_${GOARCH}/release"}
ISTIO_ENVOY_RELEASE_NAME=${ISTIO_ENVOY_RELEASE_NAME:-"envoy-$ISTIO_ENVOY_VERSION"}
ISTIO_ENVOY_RELEASE_PATH=${ISTIO_ENVOY_RELEASE_PATH:-"$ISTIO_ENVOY_RELEASE_DIR/$ISTIO_ENVOY_RELEASE_NAME"}

# Save envoy in $ISTIO_ENVOY_DIR
if [ ! -f "$ISTIO_ENVOY_DEBUG_PATH" ] || [ ! -f "$ISTIO_ENVOY_RELEASE_PATH" ] ; then
    # Set the value of DOWNLOAD_COMMAND (either curl or wget)
    set_download_command

    # Download debug envoy binary.
    mkdir -p $ISTIO_ENVOY_DEBUG_DIR
    pushd $ISTIO_ENVOY_DEBUG_DIR
    echo "Downloading envoy debug artifact: ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_DEBUG_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_DEBUG_URL} | tar xz
    cp usr/local/bin/envoy $ISTIO_ENVOY_DEBUG_PATH
    rm -rf usr
    popd

    # Download release envoy binary.
    mkdir -p $ISTIO_ENVOY_RELEASE_DIR
    pushd $ISTIO_ENVOY_RELEASE_DIR
    echo "Downloading envoy release artifact: ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_RELEASE_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_RELEASE_URL} | tar xz
    cp usr/local/bin/envoy $ISTIO_ENVOY_RELEASE_PATH
    rm -rf usr
    popd

    # Clear out any old versions of Envoy.
    rm -f ${ISTIO_OUT}/envoy ${ROOT}/pilot/pkg/proxy/envoy/envoy ${ISTIO_BIN}/envoy
fi

# TODO(nmittler): Remove once tests no longer use the envoy binary directly.
if [ ! -f ${ISTIO_OUT}/envoy ] ; then
    mkdir -p ${ISTIO_OUT}
    # Make sure the envoy binary exists. This is only used for tests, so use the debug binary.
    cp ${ISTIO_ENVOY_DEBUG_PATH} ${ISTIO_OUT}/envoy
fi

if [ ! -f /usr/local/bin/helm -a ! -f ${ISTIO_OUT}/helm ] ; then
    # Install helm. Please keep it in sync with .circleci
    cd /tmp && \
        curl -Lo /tmp/helm.tgz https://storage.googleapis.com/kubernetes-helm/helm-${HELM_VER}-linux-amd64.tar.gz && \
        tar xfz helm.tgz && \
        mv linux-amd64/helm ${ISTIO_OUT}/helm && \
        rm -rf helm.tgz linux-amd64
fi

# TODO(nmittler): Remove once tests no longer use the envoy binary directly.
# circleCI expects this in the bin directory
if [ ! -f ${ISTIO_BIN}/envoy ] ; then
    mkdir -p ${ISTIO_BIN}
    # Make sure the envoy binary exists. This is only used for tests, so use the debug binary.
    cp ${ISTIO_ENVOY_DEBUG_PATH} ${ISTIO_BIN}/envoy
fi
