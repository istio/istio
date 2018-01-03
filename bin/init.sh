#!/bin/bash

# Init script downloads or updates envoy and the go dependencies.

ROOT=$(cd $(dirname $0)/..; pwd)
ISTIO_GO=$ROOT

set -o errexit
set -o nounset
set -o pipefail
set -x

# Set GOPATH to match the expected layout
TOP=$(cd $(dirname $0)/../../../..; pwd)
OUT=${TOP}/out

export GOPATH=$TOP

# Ensure expected GOPATH setup
if [ ${ROOT} != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

# Download dependencies
if [ ! -d vendor/github.com ]; then
    if which dep; then
        echo "Using $(which dep)"
    else
        go get -u github.com/golang/dep/cmd/dep
    fi
    dep ensure
fi

# Original circleci - replaced with the version in the dockerfile, as we deprecate bazel
#ISTIO_PROXY_BUCKET=$(sed 's/ = /=/' <<< $( awk '/ISTIO_PROXY_BUCKET =/' WORKSPACE))
#PROXYVERSION=$(sed 's/[^"]*"\([^"]*\)".*/\1/' <<<  $ISTIO_PROXY_BUCKET)
PROXYVERSION=$(grep envoy-debug pilot/docker/Dockerfile.proxy_debug  |cut -d: -f2)
PROXY=debug-$PROXYVERSION

if [ ! -f $GOPATH/bin/envoy-$PROXYVERSION ] ; then
    mkdir -p $OUT
    pushd $OUT
    # TODO: Use circleci builds
    curl -Lo - https://storage.googleapis.com/istio-build/proxy/envoy-$PROXY.tar.gz | tar xz
    cp usr/local/bin/envoy $TOP/bin/envoy
    cp usr/local/bin/envoy $TOP/bin/envoy-$PROXYVERSION
    ln -sf $TOP/bin/envoy ${ROOT}/pilot/proxy/envoy/
    popd
fi


