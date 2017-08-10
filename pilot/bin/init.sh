#!/bin/bash
set -ex

# Ensure expected GOPATH setup
PDIR=`pwd`
if [ $PDIR != "$GOPATH/src/istio.io/pilot" ]; then
       echo "Pilot not found in GOPATH/src/istio.io/"
       exit 1
fi

# Building and testing with Bazel
bazel build //...

# Clean up vendor dir
rm -rf $(pwd)/vendor

# Vendorize bazel dependencies
bin/bazel_to_go.py

# Remove doubly-vendorized k8s dependencies
rm -rf vendor/k8s.io/*/vendor

# Link proto gen files
mkdir -p vendor/istio.io/api/proxy/v1/config
for f in dest_policy.pb.go  http_fault.pb.go  l4_fault.pb.go  proxy_mesh.pb.go  route_rule.pb.go ingress_rule.pb.go; do
  ln -sf $(pwd)/bazel-genfiles/external/io_istio_api/proxy/v1/config/$f \
    vendor/istio.io/api/proxy/v1/config/
done

mkdir -p vendor/istio.io/pilot/test/grpcecho
for f in $(pwd)/bazel-genfiles/test/grpcecho/*.pb.go; do
  ln -sf $f vendor/istio.io/pilot/test/grpcecho/
done

mkdir -p vendor/github.com/googleapis/googleapis/google/rpc
for f in $(pwd)/bazel-genfiles/external/com_github_googleapis_googleapis/google/rpc/*.pb.go; do
  ln -sf $f vendor/github.com/googleapis/googleapis/google/rpc/
done

# Link mock gen files
ln -sf "$(pwd)/bazel-genfiles/model/mock_config_gen_test.go" \
  model/

# Some linters expect the code to be installed
go install ./...
