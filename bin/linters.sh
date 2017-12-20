#!/bin/bash
set -ex

USE_BAZEL=${USE_BAZEL:-0}
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

if [ "$USE_BAZEL" == "1" ] ; then
  WORKSPACE="$(bazel info workspace)"
  source "${WORKSPACE}/bin/use_bazel_go.sh"
else
  WORKSPACE=$SCRIPTPATH/..
fi

cd ${WORKSPACE}

if [ "$USE_BAZEL" == "1" ] ; then
  bazel ${BAZEL_STARTUP_ARGS} build ${BAZEL_RUN_ARGS} \
    //... $(bazel query 'tests(//...)') @com_github_bazelbuild_buildtools//buildifier

  buildifier="$(bazel info bazel-bin)/external/com_github_bazelbuild_buildtools/buildifier/buildifier"
fi

NUM_CPU=$(getconf _NPROCESSORS_ONLN)

if [[ -z $SKIP_INIT ]];then
  bin/init.sh
fi

echo 'Running linters .... in advisory mode'
if [ "$USE_BAZEL" == "1" ] ; then
docker run\
  -v $(bazel info output_base):$(bazel info output_base)\
  -v $(pwd):/go/src/istio.io/istio\
  -w /go/src/istio.io/istio\
  gcr.io/mukai-istio/linter:bbcfb47f85643d4f5a7b1c092280d33ffd214c10\
  --config=./lintconfig.gen.json \
  ./...
else
docker run\
  -v $(pwd):/go/src/istio.io/istio\
  -w /go/src/istio.io/istio\
  gcr.io/mukai-istio/linter:bbcfb47f85643d4f5a7b1c092280d33ffd214c10\
  --config=./lintconfig.gen.json \
  ./...

fi
echo 'linters OK'

echo 'Checking licences'
bin/check_license.sh
echo 'licences OK'

if [ "$USE_BAZEL" == "1" ] ; then
  echo 'Running buildifier ...'
  ${buildifier} -showlog -mode=check $(git ls-files \
    | grep -e 'BUILD' -e 'WORKSPACE' -e '.*\.bazel' -e '.*\.bzl' \
    | grep -v vendor) || true
  echo 'buildifer OK'
fi