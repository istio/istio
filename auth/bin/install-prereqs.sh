#!/bin/bash

# Install linters
go get -u github.com/alecthomas/gometalinter
gometalinter --install --vendored-linters

# Install gazelle, a Go BUILD file generator
go get -u github.com/bazelbuild/rules_go/go/tools/gazelle/gazelle
