#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

detected_OS=`uname -s 2>/dev/null || echo not`
BUILD_FLAGS="--output_groups=static"
SUFFIX=".static"
if [ "${detected_OS}" == "Darwin" ]; then # Mac OS X
    BUILD_FLAGS="--cpu=darwin"
    SUFFIX=""
fi

# Ensure expected GOPATH setup
PDIR=`pwd`
if [ $PDIR != "${GOPATH-$HOME/go}/src/istio.io/pilot" ]; then
       echo "Pilot not found in GOPATH/src/istio.io/"
       exit 1
fi

# Building and testing with Bazel
bazel build ${BUILD_FLAGS} //...

source "${PDIR}/bin/use_bazel_go.sh"
go version

# Clean up vendor dir
rm -rf $(pwd)/vendor

# Vendorize bazel dependencies
bin/bazel_to_go.py

# Remove doubly-vendorized k8s dependencies
rm -rf vendor/k8s.io/*/vendor

# Link generated files
genfiles=$(bazel info bazel-genfiles)

# Link proto gen files
mkdir -p vendor/istio.io/api/proxy/v1/config
for f in dest_policy.pb.go  http_fault.pb.go  l4_fault.pb.go  proxy_mesh.pb.go  route_rule.pb.go ingress_rule.pb.go egress_rule.pb.go; do
  ln -sf $genfiles/external/io_istio_api/proxy/v1/config/$f \
    vendor/istio.io/api/proxy/v1/config/
done

# Mixer proto gen files
mkdir -p vendor/github.com/googleapis/googleapis/google/rpc
for f in code.pb.go error_details.pb.go status.pb.go; do
  ln -sf $genfiles/external/com_github_googleapis_googleapis/google/rpc/$f \
    vendor/github.com/googleapis/googleapis/google/rpc/
done

mkdir -p vendor/istio.io/pilot/test/mixer/istio_mixer_v1
ln -sf "$genfiles/test/mixer/istio_mixer_v1/mixer.pb.go" \
  vendor/istio.io/pilot/test/mixer/istio_mixer_v1/
mkdir -p vendor/istio.io/pilot/test/mixer/wordlist
ln -sf "$genfiles/test/mixer/wordlist/wordlist.go" \
  vendor/istio.io/pilot/test/mixer/wordlist/
mkdir -p vendor/istio.io/pilot/test/grpcecho
ln -sf "$genfiles/test/grpcecho/echo.pb.go" \
  vendor/istio.io/pilot/test/grpcecho/

# Link CRD generated files
ln -sf "$genfiles/adapter/config/crd/types.go" \
  adapter/config/crd/

# Link envoy binary
ln -sf "$genfiles/proxy/envoy/envoy" proxy/envoy/

# Some linters expect the code to be installed
go install ./...
