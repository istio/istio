#!/bin/bash

WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $WD)

set -o errexit
set -o nounset
set -o pipefail
set -x

# Ensure expected GOPATH setup
if [ $ROOT != "${GOPATH-$HOME/go}/src/istio.io/istio" ]; then
       echo "Istio not found in GOPATH/src/istio.io/"
       exit 1
fi

# This step is to fetch resources and create genfiles
bazel build //...

source "${ROOT}/bin/use_bazel_go.sh"

# Clean up vendor dir
rm -rf ${ROOT}/vendor
mkdir -p ${ROOT}/vendor

# Vendorize bazel dependencies
${ROOT}/bin/bazel_to_go.py ${ROOT}
genfiles=$(bazel info bazel-genfiles)
ln -sf "$genfiles/proxy/envoy/envoy" ${ROOT}/pilot/proxy/envoy/

# Remove doubly-vendorized k8s dependencies
rm -rf ${ROOT}/vendor/k8s.io/*/vendor
