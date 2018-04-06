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

if which goimports; then
  goimports=`which goimports`
else
  go get golang.org/x/tools/cmd/goimports
  goimports=${GOPATH}/bin/goimports
fi

PKGS=${PKGS:-"."}
if [[ -z ${GO_FILES} ]];then
  GO_FILES=$(find ${PKGS} -type f -name '*.go' ! -name '*.gen.go' ! -name '*.pb.go' ! -name '*mock*.go' | grep -v ./vendor)
fi

UX=$(uname)

if [ $check = false ]; then
  #remove blank lines so gofmt / goimports can do their job
  for fl in ${GO_FILES}; do
    if [[ ${UX} == "Darwin" ]]; then
      sed -i '' -e "/^import[[:space:]]*(/,/)/{ /^\s*$/d;}" $fl
    else
      sed -i -e "/^import[[:space:]]*(/,/)/{ /^\s*$/d;}" $fl
    fi
  done

  gofmt -s -w ${GO_FILES}
  $goimports -w -local istio.io ${GO_FILES}
  exit $?
fi

#check mode
#remove blank lines so gofmt / goimports can do their job
tf="/tmp/~output.go"
ec=0
for fl in ${GO_FILES}; do
  if [[ ${UX} == "Darwin" ]]; then
    sed -e "/^import[[:space:]]*(/,/)/{ /^\s*$/d;}" $fl > $tf
  else
    sed -e "/^import[[:space:]]*(/,/)/{ /^\s*$/d;}" $fl > $tf
  fi

  gofmt -s -w $tf
  $goimports -w -local istio.io $tf
  if [[ $(diff $tf $fl) ]]; then
    echo "File $fl needs formatting. Please run bin/fmt.sh"
    diff $tf $fl
    ec=1
  fi
done

#rm $tf
exit $ec
