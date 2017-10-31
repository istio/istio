#!/bin/bash
# Runs all requisite linters over the whole mixer code base.
set -o errexit
set -o nounset
set -o pipefail
set -x

bin/bazel_to_go.py

echo Running buildifier...
bazel build @com_github_bazelbuild_buildtools//buildifier
buildifier=$(bazel info bazel-bin)/external/com_github_bazelbuild_buildtools/buildifier/buildifier
$buildifier -showlog -mode=check \
    $(find . -type f \( -name 'BUILD*' -or -name 'WORKSPACE' -or -wholename '.*bazel$' -or -wholename '.*bzl$' \) -print )

echo Running linters...
LINTER_SHA="bfcc1d6942136fd86eb6f1a6fb328de8398fbd80"
if [[ "$OSTYPE" == "darwin"* ]]; then
  gometalinter=$(which gometalinter)
  if [[ -z $gometalinter ]]; then
    cat << EOF
# Please install gometalinter:
go get -d github.com/alecthomas/gometalinter && \
pushd $HOME/go/src/github.com/alecthomas/gometalinter && \
git checkout -q "${LINTER_SHA}" && \
go build -v -o $HOME/bin/gometalinter . && \
gometalinter --install && \
popd
EOF
    exit 1
  fi
else
  # Note: WriteHeaderAndJson excluded because the interface is defined in a 3rd party library.
  gometalinter="docker run \
    -v $(bazel info output_base):$(bazel info output_base) \
    -v $(pwd):/go/src/istio.io/istio/mixer \
    -w /go/src/istio.io/istio/mixer \
    gcr.io/istio-testing/linter:${LINTER_SHA}"
fi

NUM_CPU=$(getconf _NPROCESSORS_ONLN)
$gometalinter \
    --concurrency=${NUM_CPU}\
    --enable-gc\
    --vendored-linters\
    --deadline=1200s --disable-all\
    --enable=aligncheck\
    --enable=deadcode\
    --enable=errcheck\
    --enable=gas\
    --enable=goconst\
    --enable=gofmt\
    --enable=goimports\
    --enable=golint --min-confidence=0\
    --enable=gotype\
    --exclude=vendor\
    --exclude=.pb.go\
    --exclude=pkg/config/proto/combined.go\
    --exclude=.*.gen.go\
    --exclude="should have a package comment"\
    --exclude=".*pkg/config/apiserver_test.go:.* method WriteHeaderAndJson should be WriteHeaderAndJSON"\
    --enable=ineffassign\
    --enable=interfacer\
    --enable=lll --line-length=160\
    --enable=megacheck\
    --enable=misspell\
    --enable=structcheck\
    --enable=unconvert\
    --enable=unparam\
    --enable=varcheck\
    --enable=vet\
    --enable=vetshadow\
    --skip=testdata\
    --skip=vendor\
    --vendor\
    ./...

echo Check license...
bin/check_license.sh

echo Check workspace...
bin/check_workspace.sh

echo Checking gazelle...
bin/gazelle
builddiff=$(git diff --name-only -- `find . -type f \( -name BUILD -o -name BUILD.bazel \)`)
if [[ $builddiff ]]; then
  echo Change in BUILD files detected: $builddiff
  exit 1
fi

echo Done running linters
