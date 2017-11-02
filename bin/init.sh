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

bazel build //...

source "${ROOT}/bin/use_bazel_go.sh"

# Clean up vendor dir
rm -rf ${ROOT}/vendor
mkdir -p ${ROOT}/vendor

# Vendorize bazel dependencies
${ROOT}/mixer/bin/bazel_to_go.py ${ROOT}

# link Pilot specific stuff
genfiles=$(bazel info bazel-genfiles)
# Link CRD generated files
ln -sf "$genfiles/pilot/adapter/config/crd/types.go" \
  ${ROOT}/pilot/adapter/config/crd/

# Link envoy binary
ln -sf "$genfiles/pilot/proxy/envoy/envoy" ${ROOT}/pilot/proxy/envoy/

ln -sf "$genfiles/security/proto/ca_service.pb.go" ${ROOT}/security/proto

# Remove doubly-vendorized k8s dependencies
rm -rf {ROOT}/vendor/k8s.io/*/vendor


