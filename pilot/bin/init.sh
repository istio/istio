#!/bin/bash
set -ex

# Vendorize bazel dependencies
bin/bazel_to_go.py > /dev/null

# Remove doubly-vendorized k8s dependencies
rm -rf vendor/k8s.io/client-go/vendor

# Link proto gen files
mkdir -p vendor/istio.io/api/proxy/v1/config
ln -sf "$(pwd)/bazel-genfiles/external/io_istio_api/proxy/v1/config/cfg.pb.go" \
  vendor/istio.io/api/proxy/v1/config/cfg.pb.go

# Link mock gen files
ln -sf "$(pwd)/bazel-genfiles/model/mock_config_gen_test.go" \
  model/

# Some linters expect the code to be installed
go install ./...
