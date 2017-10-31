#!/bin/bash

set -ex

echo "Installing or updating linters"
go get -u github.com/alecthomas/gometalinter
gometalinter --install --update --vendored-linters
echo "Done installing linters"

# Install buildifier BUILD file validator
go get -u github.com/bazelbuild/buildifier/buildifier
