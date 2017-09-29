#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

# Install linters
go get -u github.com/alecthomas/gometalinter
gometalinter --install --update --vendored-linters

# Install buildifier BUILD file validator
go get -u github.com/bazelbuild/buildifier/buildifier
