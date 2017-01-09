#!/bin/bash

# Applies requisite code formatters to the source tree

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

gofmt -s -w adapter cmd pkg
goimports -w adapter cmd pkg
buildifier -mode=fix $(find adapter cmd pkg -name BUILD -type f)
buildifier -mode=fix ./BUILD
buildifier -mode=fix ./BUILD.api
