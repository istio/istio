#!/bin/bash

# Install mockgen tool to generate golang mock interfaces
go get github.com/golang/mock/mockgen

# Install linters
go get -u github.com/alecthomas/gometalinter
gometalinter --install --vendored-linters

# Install buildifier BUILD file validator
go get -u github.com/bazelbuild/buildifier/buildifier
