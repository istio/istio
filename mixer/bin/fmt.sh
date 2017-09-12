#!/bin/bash

# Applies requisite code formatters to the source tree

set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

GO_FILES=$(find adapter cmd example tools/codegen test pkg template -type f -name '*.go')

UX=$(uname)

#remove blank lines so gofmt / goimports can do their job
for fl in ${GO_FILES}; do
	if [[ ${UX} == "Darwin" ]];then
		sed -i '' -e "/^import[[:space:]]*(/,/)/{ /^\s*$/d;}" $fl
	else
		sed -i -e "/^import[[:space:]]*(/,/)/{ /^\s*$/d;}" $fl
	fi
done
gofmt -s -w ${GO_FILES}
goimports -w -local istio.io ${GO_FILES}
buildifier -mode=fix $(find . \( -name BUILD -o -name BUILD.bazel \) -type f)
buildifier -mode=fix ./*.bzl
buildifier -mode=fix ./BUILD.ubuntu
buildifier -mode=fix ./WORKSPACE
