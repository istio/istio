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
  DOWNLOAD_COMMAND="${ROOT}/bin/s3_download.sh"
  # Aspenmesh uses s3.
  if ! aws s3 help > /dev/null 2>&1; then
    if [ "$CIRCLECI" == "true" ]; then
      sudo apt update && sudo apt install awscli
      DOWNLOAD_COMMAND="${ROOT}/bin/s3_download.sh"
    else
      echo "aws cli tools are required"
      exit 1
    fi
  fi
}

if [ -z ${PROXY_REPO_SHA:-} ] ; then
  export PROXY_REPO_SHA=$(cat aspenmesh.proxy.sha)
fi

# Normally set by the Makefile.
ISTIO_ENVOY_VERSION=${ISTIO_ENVOY_VERSION:-${PROXY_REPO_SHA}}
ISTIO_ENVOY_DEBUG_URL=${ISTIO_ENVOY_DEBUG_URL:-https://storage.googleapis.com/istio-build/proxy/envoy-debug-${ISTIO_ENVOY_VERSION}.tar.gz}
ISTIO_ENVOY_RELEASE_URL=${ISTIO_ENVOY_RELEASE_URL:-https://storage.googleapis.com/istio-build/proxy/envoy-alpha-${ISTIO_ENVOY_VERSION}.tar.gz}
# TODO Change url when official envoy release for MAC is available
ISTIO_ENVOY_MAC_RELEASE_URL=${ISTIO_ENVOY_MAC_RELEASE_URL:-https://storage.googleapis.com/istio-on-macos/releases/0.7.1/istio-proxy-0.7.1-macos.tar.gz}

# Normally set by the Makefile.
# Variables for the extracted debug/release Envoy artifacts.
ISTIO_ENVOY_DEBUG_DIR=${ISTIO_ENVOY_DEBUG_DIR:-"${OUT_DIR}/${GOOS}_${GOARCH}/debug"}
ISTIO_ENVOY_DEBUG_NAME=${ISTIO_ENVOY_DEBUG_NAME:-"envoy-debug-$ISTIO_ENVOY_VERSION"}
ISTIO_ENVOY_DEBUG_PATH=${ISTIO_ENVOY_DEBUG_PATH:-"$ISTIO_ENVOY_DEBUG_DIR/$ISTIO_ENVOY_DEBUG_NAME"}
ISTIO_ENVOY_RELEASE_DIR=${ISTIO_ENVOY_RELEASE_DIR:-"${OUT_DIR}/${GOOS}_${GOARCH}/release"}
ISTIO_ENVOY_RELEASE_NAME=${ISTIO_ENVOY_RELEASE_NAME:-"envoy-$ISTIO_ENVOY_VERSION"}
ISTIO_ENVOY_RELEASE_PATH=${ISTIO_ENVOY_RELEASE_PATH:-"$ISTIO_ENVOY_RELEASE_DIR/$ISTIO_ENVOY_RELEASE_NAME"}

# Set the value of DOWNLOAD_COMMAND (either curl or wget)
set_download_command

# Save envoy in $ISTIO_ENVOY_DIR
if [ ! -f "$ISTIO_ENVOY_DEBUG_PATH" ] || [ ! -f "$ISTIO_ENVOY_RELEASE_PATH" ] ; then
    # Clear out any old versions of Envoy.
    rm -f ${ISTIO_OUT}/envoy ${ROOT}/pilot/pkg/proxy/envoy/envoy ${ISTIO_BIN}/envoy

    # Download debug envoy binary.
    mkdir -p $ISTIO_ENVOY_DEBUG_DIR
    pushd $ISTIO_ENVOY_DEBUG_DIR
    if [ "$LOCAL_OS" == "Darwin" ] && [ "$GOOS" != "linux" ]; then
       ISTIO_ENVOY_DEBUG_URL=${ISTIO_ENVOY_MAC_RELEASE_URL}
    fi
    echo "Downloading envoy debug artifact: ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_DEBUG_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_DEBUG_URL} | tar xz
    cp usr/local/bin/envoy $ISTIO_ENVOY_DEBUG_PATH
    rm -rf usr
    popd

    # Download release envoy binary.
    mkdir -p $ISTIO_ENVOY_RELEASE_DIR
    pushd $ISTIO_ENVOY_RELEASE_DIR
    if [ "$LOCAL_OS" == "Darwin" ] && [ "$GOOS" != "linux" ]; then
       ISTIO_ENVOY_RELEASE_URL=${ISTIO_ENVOY_MAC_RELEASE_URL}
    fi
    echo "Downloading envoy release artifact: ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_RELEASE_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_RELEASE_URL} | tar xz
    cp usr/local/bin/envoy $ISTIO_ENVOY_RELEASE_PATH
    rm -rf usr
    popd
fi

mkdir -p ${ISTIO_OUT}
mkdir -p ${ISTIO_BIN}

# copy debug envoy binary used for local tests such as ones in mixer/test/clients
if [ "$LOCAL_OS" == "Darwin" ]; then
    # Download darwin envoy binary.
    DARWIN_ENVOY_DIR=$(mktemp -d)
    pushd $DARWIN_ENVOY_DIR
    echo "Downloading envoy darwin binary: ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_MAC_RELEASE_URL}"
    time ${DOWNLOAD_COMMAND} ${ISTIO_ENVOY_MAC_RELEASE_URL} | tar xz
    cp usr/local/bin/envoy ${ISTIO_OUT}/envoy
    cp usr/local/bin/envoy ${ISTIO_BIN}/envoy
    popd
    rm -rf $DARWIN_ENVOY_DIR
else
    cp -f ${ISTIO_ENVOY_DEBUG_PATH} ${ISTIO_OUT}/envoy
    # TODO(nmittler): Remove once tests no longer use the envoy binary directly.
    # circleCI expects this in the bin directory
    # Make sure the envoy binary exists. This is only used for tests, so use the debug binary.
    cp ${ISTIO_ENVOY_DEBUG_PATH} ${ISTIO_BIN}/envoy
fi

sha256sum ${ISTIO_OUT}/envoy || true
if [ "$GOOS" == "$LOCAL_OS" ]; then
  ${ISTIO_OUT}/envoy --version
fi

${ROOT}/bin/init_helm.sh
