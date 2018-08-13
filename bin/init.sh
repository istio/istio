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

# Init script downloads or updates mosn and the go dependencies. Called from Makefile, which sets
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

export GOPATH=${GOPATH:-$GO_TOP}
# Normally set by Makefile
export ISTIO_BIN=${ISTIO_BIN:-${GOPATH}/bin}

# Set the architecture. Matches logic in the Makefile.
export GOARCH=${GOARCH:-'amd64'}

# Determine the OS. Matches logic in the Makefile.
LOCAL_OS=${LOCAL_OS:-"`uname`"}
case $LOCAL_OS in
  'Linux')
    export GOOS=${GOOS:-"linux"}
    ;;
  'Darwin')
    export GOOS=${GOOS:-"darwin"}
    ;;
  *)
    echo "This system's OS ${LOCAL_OS} isn't recognized/supported"
    exit 1
    ;;
esac

# test scripts seem to like to run this script directly rather than use make
export ISTIO_OUT=${ISTIO_OUT:-${ISTIO_BIN}}

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
         Cannot download mosn. Please install wget or add support of https to curl.
    exit 1
}

if [ -z ${PROXY_REPO_SHA:-} ] ; then
  export PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
fi

# Normally set by the Makefile.
ISTIO_MOSN_VERSION=${ISTIO_MOSN_VERSION:-${PROXY_REPO_SHA}}
ISTIO_MOSN_DEBUG_URL=${ISTIO_MOSN_DEBUG_URL:-http://storage.googleapis.com/sofa-mesh/proxy/mosn-debug-${ISTIO_MOSN_VERSION}.tar.gz}
ISTIO_MOSN_RELEASE_URL=${ISTIO_MOSN_RELEASE_URL:-http://storage.googleapis.com/sofa-mesh/proxy/mosn-alpha-${ISTIO_MOSN_VERSION}.tar.gz}
# TODO Change url when official mosn release for MAC is available
ISTIO_MOSN_MAC_RELEASE_URL=${ISTIO_MOSN_MAC_RELEASE_URL:-https://storage.googleapis.com/istio-on-macos/releases/0.7.1/istio-proxy-0.7.1-macos.tar.gz}

# Normally set by the Makefile.
# Variables for the extracted debug/release Envoy artifacts.
ISTIO_MOSN_DEBUG_DIR=${ISTIO_MOSN_DEBUG_DIR:-"${OUT_DIR}/${GOOS}_${GOARCH}/debug"}
ISTIO_MOSN_DEBUG_NAME=${ISTIO_MOSN_DEBUG_NAME:-"mosn-debug-$ISTIO_MOSN_VERSION"}
ISTIO_MOSN_DEBUG_PATH=${ISTIO_MOSN_DEBUG_PATH:-"$ISTIO_MOSN_DEBUG_DIR/$ISTIO_MOSN_DEBUG_NAME"}
ISTIO_MOSN_RELEASE_DIR=${ISTIO_MOSN_RELEASE_DIR:-"${OUT_DIR}/${GOOS}_${GOARCH}/release"}
ISTIO_MOSN_RELEASE_NAME=${ISTIO_MOSN_RELEASE_NAME:-"mosn-$ISTIO_MOSN_VERSION"}
ISTIO_MOSN_RELEASE_PATH=${ISTIO_MOSN_RELEASE_PATH:-"$ISTIO_MOSN_RELEASE_DIR/$ISTIO_MOSN_RELEASE_NAME"}

# Set the value of DOWNLOAD_COMMAND (either curl or wget)
set_download_command

# Save mosn in $ISTIO_MOSN_DIR
if [ ! -f "$ISTIO_MOSN_DEBUG_PATH" ] || [ ! -f "$ISTIO_MOSN_RELEASE_PATH" ] ; then
    # Clear out any old versions of Envoy.
    rm -f ${ISTIO_OUT}/mosn ${ROOT}/pilot/pkg/proxy/mosn/mosn ${ISTIO_BIN}/mosn

    # Download debug mosn binary.
    mkdir -p $ISTIO_MOSN_DEBUG_DIR
    pushd $ISTIO_MOSN_DEBUG_DIR
    if [ "$LOCAL_OS" == "Darwin" ] && [ "$GOOS" != "linux" ]; then
       ISTIO_MOSN_DEBUG_URL=${ISTIO_MOSN_MAC_RELEASE_URL}
    fi
    echo "Downloading mosn debug artifact: ${DOWNLOAD_COMMAND} ${ISTIO_MOSN_DEBUG_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_MOSN_DEBUG_URL} | tar xz
    cp usr/local/bin/mosn $ISTIO_MOSN_DEBUG_PATH
    rm -rf usr
    popd

    # Download release mosn binary.
    mkdir -p $ISTIO_MOSN_RELEASE_DIR
    pushd $ISTIO_MOSN_RELEASE_DIR
    if [ "$LOCAL_OS" == "Darwin" ] && [ "$GOOS" != "linux" ]; then
       ISTIO_MOSN_RELEASE_URL=${ISTIO_MOSN_MAC_RELEASE_URL}
    fi
    echo "Downloading mosn release artifact: ${DOWNLOAD_COMMAND} ${ISTIO_MOSN_RELEASE_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_MOSN_RELEASE_URL} | tar xz
    cp usr/local/bin/mosn $ISTIO_MOSN_RELEASE_PATH
    rm -rf usr
    popd
fi

mkdir -p ${ISTIO_OUT}
mkdir -p ${ISTIO_BIN}

# copy debug mosn binary used for local tests such as ones in mixer/test/clients
if [ "$LOCAL_OS" == "Darwin" ]; then
    # Download darwin mosn binary.
    DARWIN_MOSN_DIR=$(mktemp -d)
    pushd $DARWIN_MOSN_DIR
    echo "Downloading mosn darwin binary: ${DOWNLOAD_COMMAND} ${ISTIO_MOSN_MAC_RELEASE_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_MOSN_MAC_RELEASE_URL} | tar xz
    cp usr/local/bin/mosn ${ISTIO_OUT}/mosn
    cp usr/local/bin/mosn ${ISTIO_BIN}/mosn
    popd
    rm -rf $DARWIN_MOSN_DIR
else
    cp -f ${ISTIO_MOSN_DEBUG_PATH} ${ISTIO_OUT}/mosn
    # TODO(nmittler): Remove once tests no longer use the mosn binary directly.
    # circleCI expects this in the bin directory
    # Make sure the mosn binary exists. This is only used for tests, so use the debug binary.
    cp ${ISTIO_MOSN_DEBUG_PATH} ${ISTIO_BIN}/mosn
fi

${ROOT}/bin/init_helm.sh
