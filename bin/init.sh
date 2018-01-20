#!/bin/bash

# Init script downloads or updates envoy and the go dependencies. Called from Makefile, which sets
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
export ISTIO_BIN=${ISTIO_BIN:-${GOPATH}/bin}

# Ensure GO version
GO_VERSION=$(go version | sed 's/go version go\([[:digit:]]\.[[:digit:]]\.[[:digit:]]\).*/\1/g')
GO_VERSION_MAJOR=$(echo $GO_VERSION | cut -f1 -d.)
GO_VERSION_MINOR=$(echo $GO_VERSION | cut -f2 -d.)
if [ "$GO_VERSION_MAJOR" -ne "1" -o "$GO_VERSION_MINOR" -ne "9" ]; then
    echo Go version is ${GO_VERSION}. Istio compiles with Go 1.9. Please update your go.
    exit 1
fi

# Ensure expected GOPATH setup
if [ ${ROOT} != "${GO_TOP:-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

DEP=${DEP:-$(which dep || echo "${ISTIO_BIN}/dep" )}

# Just in case init.sh is called directly, not from Makefile which has a dependency to dep
if [ ! -f ${DEP} ]; then
    DEP=${ISTIO_BIN}/dep
    unset GOOS && go get -u github.com/golang/dep/cmd/dep
fi

# Download dependencies if needed
if [ ! -d vendor/github.com ]; then
    ${DEP} ensure -vendor-only
    cp Gopkg.lock vendor/Gopkg.lock
elif [ ! -f vendor/Gopkg.lock ]; then
    ${DEP} ensure -vendor-only
    cp Gopkg.lock vendor/Gopkg.lock
else
    diff Gopkg.lock vendor/Gopkg.lock > /dev/null || \
            ( ${DEP} ensure -vendor-only ; \
              cp Gopkg.lock vendor/Gopkg.lock)
fi

# Original circleci - replaced with the version in the dockerfile, as we deprecate bazel
#ISTIO_PROXY_BUCKET=$(sed 's/ = /=/' <<< $( awk '/ISTIO_PROXY_BUCKET =/' WORKSPACE))
#PROXYVERSION=$(sed 's/[^"]*"\([^"]*\)".*/\1/' <<<  $ISTIO_PROXY_BUCKET)
PROXYVERSION=$(grep envoy-debug pilot/docker/Dockerfile.proxy_debug  |cut -d: -f2)
PROXY=debug-$PROXYVERSION

# Save envoy in vendor, which is cached
if [ ! -f vendor/envoy-$PROXYVERSION ] ; then
    mkdir -p $OUT
    pushd $OUT
    # New version of envoy downloaded. Save it to cache, and clean any old version.
    curl -Lo - https://storage.googleapis.com/istio-build/proxy/envoy-$PROXY.tar.gz | tar xz
    cp usr/local/bin/envoy $ISTIO_GO/vendor/envoy-$PROXYVERSION
    rm -f ${ISTIO_BIN}/envoy ${ROOT}/pilot/proxy/envoy/envoy
    popd
fi

if [ ! -f $GO_TOP/bin/envoy ] ; then
    mkdir -p $GO_TOP/bin
    # Make sure the envoy binary exists.
    cp $ISTIO_GO/vendor/envoy-$PROXYVERSION ${ISTIO_BIN}/envoy
fi

# Deprecated, may still be used in some tests
if [ ! -f ${ROOT}/pilot/proxy/envoy/envoy ] ; then
    ln -sf ${ISTIO_BIN}/envoy ${ROOT}/pilot/proxy/envoy
fi
