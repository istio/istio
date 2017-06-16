#!/usr/bin/env bash

set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR


echo "Race test"
# TODO remove the exclusion of codegen once the codegen tests start working via bazel
go test -race `go list ./... | grep -v codegen`
