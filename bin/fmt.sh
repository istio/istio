#!/bin/bash

# Applies requisite code formatters to the source tree

set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

export GOPATH=$(cd $ROOTDIR/../../..; pwd)
export PATH=$GOPATH/bin:$PATH

if which goimports; then
  goimports=`which goimports`
else
  go get golang.org/x/tools/cmd/goimports
  goimports=${GOPATH}/bin/goimports
fi

PKGS=${PKGS:-"."}

GO_FILES=$(find ${PKGS} -type f -name '*.go' ! -name '*.gen.go' ! -name '*.pb.go' ! -name '*mock*.go' | grep -v ./vendor)

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
$goimports -w -local istio.io ${GO_FILES}
