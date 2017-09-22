#!/bin/bash

# This file takes care of linking generated files into the proper path for lint.
# This is a prerequist to run linters.

set -ex

# Ensure expected GOPATH setup
PDIR=`pwd`
if [ $PDIR != "${GOPATH-$HOME/go}/src/istio.io/broker" ]; then
  echo "Broker not found in GOPATH/src/istio.io/"
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
mkdir -p vendor/istio.io/api/broker/v1/config
for f in service_class.pb.go  service_plan.pb.go; do
  ln -sf $(pwd)/bazel-genfiles/external/io_istio_api/broker/v1/config/$f \
    vendor/istio.io/api/broker/v1/config/
done

ln -sf $(pwd)/bazel-genfiles/pkg/testing/mock/proto/fake_config.pb.go \
  pkg/testing/mock/proto/

# Link CRD generated files
ln -sf "$(pwd)/bazel-genfiles/pkg/platform/kube/crd/types.go" \
  pkg/platform/kube/crd/

# Some linters expect the code to be installed
go install ./...
