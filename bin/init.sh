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

# Init script downloads or updates envoy. Called from Makefile, which sets
# the needed environment variables.

ROOT=$(cd $(dirname $0)/..; pwd)
ISTIO_GO=$ROOT

set -o errexit
set -o nounset
set -o pipefail

# Set GOPATH to match the expected layout
GO_TOP=$(cd $(dirname $0)/../../../..; pwd)
OUT=${GO_TOP}/out

export GOPATH=${GOPATH:-$GO_TOP}
# Normally set by Makefile
export ISTIO_BIN=${ISTIO_BIN:-${GO_TOP}/bin}

# test scripts seem to like to run this script directly rather than use make
export ISTIO_OUT=${ISTIO_OUT:-${ISTIO_BIN}}

# Ensure expected GOPATH setup
if [ ${ROOT} != "${GO_TOP:-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

PROXYVERSION=$(grep envoy-debug pilot/docker/Dockerfile.proxy_debug  |cut -d: -f2)
PROXY=debug-$PROXYVERSION

if [ ! -f ${ISTIO_OUT}/envoy-$PROXYVERSION ] ; then
    # New version of envoy downloaded. Save it to cache, and clean any old version.

    DOWNLOAD_COMMAND=""
    if command -v curl > /dev/null; then
       if curl --version | grep Protocols  | grep https; then
	   DOWNLOAD_COMMAND='curl -Lo -'
       else
           echo curl does not support https, will try wget for downloading files.
       fi
    else
       echo curl is not installed, will try wget for downloading files.
    fi

    if [ -z "${DOWNLOAD_COMMAND}" ]; then
        if command -v wget > /dev/null; then
	    DOWNLOAD_COMMAND='wget -qO -'
        else
            echo wget is not installed.
        fi
    fi

    if [ -z "${DOWNLOAD_COMMAND}" ]; then
        echo Error: curl is not installed or does not support https, wget is not installed. \
             Cannot download envoy. Please install wget or add support of https to curl.
        exit 1
    fi

    mkdir -p $OUT
    pushd $OUT
    echo "Downloading envoy $PROXY $PROXYVERSION using $DOWNLOAD_COMMAND"
    ${DOWNLOAD_COMMAND} https://storage.googleapis.com/istio-build/proxy/envoy-$PROXY.tar.gz | tar xz
    rm -f ${ISTIO_OUT}/envoy*

    mkdir -p ${ISTIO_OUT}
    cp usr/local/bin/envoy ${ISTIO_OUT}/envoy-$PROXYVERSION
    popd

    # Make sure the envoy binary exists.
    cp ${ISTIO_OUT}/envoy-$PROXYVERSION ${ISTIO_OUT}/envoy

    # circleCI expects this in the bin directory
    mkdir -p ${ISTIO_BIN}
    cp ${ISTIO_OUT}/envoy-$PROXYVERSION ${ISTIO_BIN}/envoy
fi
