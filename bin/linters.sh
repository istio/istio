#!/bin/bash
set -ex

WORKSPACE="$(bazel info workspace)"
source "${WORKSPACE}/bin/use_bazel_go.sh"

cd ${WORKSPACE}

bazel ${BAZEL_STARTUP_ARGS} build ${BAZEL_RUN_ARGS} \
  //... $(bazel query 'tests(//...)') @com_github_bazelbuild_buildtools//buildifier

buildifier="$(bazel info bazel-bin)/external/com_github_bazelbuild_buildtools/buildifier/buildifier"

NUM_CPU=$(getconf _NPROCESSORS_ONLN)

if [[ -z $SKIP_INIT ]];then
  bin/init.sh
fi

echo 'Running linters .... in advisory mode'
docker run\
  -v $(bazel info output_base):$(bazel info output_base)\
  -v $(pwd):/go/src/istio.io/istio\
  -w /go/src/istio.io/istio\
  gcr.io/istio-testing/linter:bfcc1d6942136fd86eb6f1a6fb328de8398fbd80\
  --config=./lintconfig.json \
  ./... || true
echo 'linters OK'

echo 'Checking licences'
bin/check_license.sh || true
echo 'licences OK'

echo 'Running buildifier ...'
${buildifier} -showlog -mode=check $(git ls-files \
  | grep -e 'BUILD' -e 'WORKSPACE' -e '.*\.bazel' -e '.*\.bzl' \
  | grep -v vendor) || true
echo 'buildifer OK'
