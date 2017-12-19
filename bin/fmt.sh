#!/bin/bash

# Applies requisite code formatters to the source tree

set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
USE_BAZEL=${USE_BAZEL:-1}
if [ "$USE_BAZEL" == "1" ] ; then
  source $SCRIPTPATH/use_bazel_go.sh
fi

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

export GOPATH=$(cd $ROOTDIR/../..; pwd)
export PATH=$GOPATH/bin:$PATH

if which goimports; then
  goimports=`which goimports`
else
  go get golang.org/x/tools/cmd/goimports
  goimports=${GOPATH}/bin/goimports
fi

if which buildifier; then
  buildifier=`which buildifier`
else
  # Only use buildifier if bazel-bin is present (for the transition period)
  if [ "$USE_BAZEL" == "1" ] ; then
    bazel build @com_github_bazelbuild_buildtools//buildifier
    buildifier=$ROOTDIR/bazel-bin/external/com_github_bazelbuild_buildtools/buildifier/buildifier
  fi
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
if [ "$USE_BAZEL" == "1" ] ; then
  $buildifier -mode=fix $(git ls-files | grep -e 'BUILD' -e 'WORKSPACE' -e 'BUILD.bazel' -e '.*\.bazel' -e '.*\.bzl')
fi