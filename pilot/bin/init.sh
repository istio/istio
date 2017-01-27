#!/bin/bash
set -ex

# Vendorize bazel dependencies
bin/bazel_to_go.py > /dev/null

# Remove doubly-vendorized k8s dependencies
rm -rf vendor/k8s.io/client-go/vendor

go install ./...
go get -u github.com/alecthomas/gometalinter
gometalinter --install --vendored-linters
