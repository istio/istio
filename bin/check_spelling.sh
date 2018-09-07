#!/bin/bash

# Applies requisite code formatters to the source tree
# check_spelling.sh

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
cd "$ROOTDIR"

GOPATH=$(cd "$ROOTDIR/../../.."; pwd)
export GOPATH
export PATH=$GOPATH/bin:$PATH

# Install tools we need, but only from vendor/...
go install istio.io/istio/vendor/github.com/client9/misspell/cmd/misspell

# Spell checking
# All the skipping files are defined in bin/.spelling_failures
skipping_file="${ROOTDIR}/bin/.spelling_failures"
failing_packages=$(echo `cat ${skipping_file}` | sed "s| | -e |g")
git ls-files | grep -v -e ${failing_packages} | xargs misspell -i "Creater,creater,ect" -error -o stderr
