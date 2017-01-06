#!/bin/bash

## Does the work necessary to prepare to execute
## the various linters that are used over the
## mixer source base.

set -ev

go get -u github.com/alecthomas/gometalinter
go get -u github.com/bazelbuild/buildifier/buildifier
go get -u github.com/3rf/codecoroner
gometalinter --install >/dev/null
bin/bazel_to_go.py
