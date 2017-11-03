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

# getting the full list of generated files -- they should be excluded.
exclude_generated=`cat ${WORKSPACE}/generated_files | grep -v '^#' | sed -e 's/^/--exclude=/'`

echo 'Running linters .... in advisory mode'
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
  $exclude_generated\
  --exclude=vendor\
  --exclude=.pb.go\
  --exclude=mixer/pkg/config/proto/combined.go\
  --exclude=.*.gen.go\
  --exclude="should have a package comment"\
  --exclude=".*pkg/config/apiserver_test.go:.* method WriteHeaderAndJson should be WriteHeaderAndJSON"\
  --exclude="genfiles"\
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
