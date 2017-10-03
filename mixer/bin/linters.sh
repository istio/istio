#!/bin/bash

# Runs all requisite linters over the whole mixer code base.
set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

prep_linters() {
    bin/bazel_to_go.py
}

go_metalinter() {
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
        -v $(pwd):/go/src/istio.io/mixer \
        -w /go/src/istio.io/mixer \
        gcr.io/istio-testing/linter:${LINTER_SHA}"
    fi

    NUM_CPU=$(getconf _NPROCESSORS_ONLN)
    echo Running linters...
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
}

run_linters() {
    echo Running buildifier...
    bazel build @com_github_bazelbuild_buildtools//buildifier
    buildifier=$(bazel info bazel-bin)/external/com_github_bazelbuild_buildtools/buildifier/buildifier
    $buildifier -showlog -mode=check $(find . -name BUILD -type f)
    $buildifier -showlog -mode=check $(find . -name BUILD.bazel -type f)
    $buildifier -showlog -mode=check ./BUILD.ubuntu
    $buildifier -showlog -mode=check ./WORKSPACE
    go_metalinter
    $SCRIPTPATH/check_license.sh
    $SCRIPTPATH/check_workspace.sh
}

prep_linters
run_linters

echo Done running linters
