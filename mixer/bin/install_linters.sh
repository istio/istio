#!/bin/bash

echo "Installing or updating linters"
go get -u github.com/alecthomas/gometalinter
go get -u github.com/bazelbuild/buildifier/buildifier
go get -u github.com/3rf/codecoroner
gometalinter --install --vendored-linters >/dev/null

echo Done installing linters
