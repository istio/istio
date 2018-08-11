#!/bin/bash

# Applies requisite code formatters to the source tree
# fmt.sh -c check only.

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

check=false

case $1 in
    -c|--check)
    check=true
esac

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

export GOPATH=$(cd $ROOTDIR/../../..; pwd)
export PATH=$GOPATH/bin:$PATH

go get -u golang.org/x/tools/cmd/goimports
goimports=${GOPATH}/bin/goimports

PKGS=${PKGS:-"."}
if [[ -z ${GO_FILES} ]];then
  GO_FILES=$(find ${PKGS} -type f -name '*.go' ! -name '*.gen.go' ! -name '*.pb.go' ! -name '*mock*.go' | grep -v ./vendor)
fi

UX=$(uname)

if [ $check = false ]; then
  $goimports -w -local istio.io ${GO_FILES}
  exit $?
fi

for fl in ${GO_FILES}; do
  file_needs_formatting=$($goimports -l -local istio.io $fl)
  if [[ ! -z "$file_needs_formatting" ]]; then
    echo "please run bin/fmt.sh against: $file_needs_formatting"
    exit 1
  fi
done
