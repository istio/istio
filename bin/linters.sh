#!/bin/bash
set -e

WORKSPACE="$(bazel info workspace)"
source "${WORKSPACE}/bin/use_bazel_go.sh"

cd ${WORKSPACE}

bazel ${BAZEL_STARTUP_ARGS} build ${BAZEL_RUN_ARGS} \
  //... $(bazel query 'tests(//...)') @com_github_bazelbuild_buildtools//buildifier

buildifier="$(bazel info bazel-bin)/external/com_github_bazelbuild_buildtools/buildifier/buildifier"

NUM_CPU=$(getconf _NPROCESSORS_ONLN)

bin/bazel_to_go.py

echo 'Running linters ....'
docker run\
  -v $(bazel info output_base):$(bazel info output_base)\
  -v $(pwd):/go/src/istio.io/istio\
  -w /go/src/istio.io/istio\
  gcr.io/istio-testing/linter:bfcc1d6942136fd86eb6f1a6fb328de8398fbd80\
  --concurrency=4\
  --enable-gc\
  --vendored-linters\
  --deadline=600s --disable-all\
  --enable=aligncheck\
  --enable=deadcode\
  --enable=errcheck\
  --enable=gas\
  --enable=goconst\
  --enable=gofmt\
  --enable=goimports\
  --enable=golint --min-confidence=0 \
  --exclude=vendor/ --exclude=.pb.go --exclude="should have a package comment"\
  --enable=ineffassign\
  --enable=interfacer\
  --enable=lll --line-length=160\
  --enable=megacheck\
  --enable=misspell\
  --enable=structcheck\
  --enable=unconvert\
  --enable=varcheck\
  --enable=vet\
  --enable=vetshadow\
  ./...
echo 'linters OK'

echo 'Checking licences'
bin/check_license.sh
echo 'licences OK'

echo 'Running buildifier ...'
${buildifier} -showlog -mode=check $(git ls-files \
  | grep -e 'BUILD' -e 'WORKSPACE' -e '.*\.bazel' -e '.*\.bzl' \
  | grep -v vendor)
echo 'buildifer OK'
