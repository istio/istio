#!/bin/bash
set -ex

# Building with Bazel
bazel build //...

# Vendorize bazel dependencies
bin/bazel_to_go.py > /dev/null

# Remove doubly-vendorized k8s dependencies
rm -rf vendor/k8s.io/client-go/vendor
rm -rf vendor/k8s.io/apimachinery/vendor
rm -rf vendor/k8s.io/ingress/vendor

# Link proto gen files
mkdir -p vendor/istio.io/api/proxy/v1/config
ln -sf $(pwd)/bazel-genfiles/external/io_istio_api/proxy/v1/config/*.pb.go \
  vendor/istio.io/api/proxy/v1/config/
mkdir -p vendor/istio.io/manager/test/grpcecho
ln -sf $(pwd)/bazel-genfiles/test/grpcecho/*.pb.go \
   vendor/istio.io/manager/test/grpcecho/
mkdir -p vendor/github.com/googleapis/googleapis/google/rpc
ln -sf $(pwd)/bazel-genfiles/external/com_github_googleapis_googleapis/google/rpc/*.pb.go \
   vendor/github.com/googleapis/googleapis/google/rpc/

# Link mock gen files
ln -sf "$(pwd)/bazel-genfiles/model/mock_config_gen_test.go" \
  model/

# Some linters expect the code to be installed
go install ./...
