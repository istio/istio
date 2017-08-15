#!/bin/bash

set -ex

# Install linters
go get -u github.com/alecthomas/gometalinter
gometalinter --install --update --vendored-linters

# Install buildifier BUILD file validator
go get -u github.com/bazelbuild/buildifier/buildifier
