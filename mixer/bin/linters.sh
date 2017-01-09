#!/bin/bash

# Runs all requisite linters over the whole mixer code base.

prep_linters() {
    go get -u github.com/alecthomas/gometalinter
    go get -u github.com/bazelbuild/buildifier/buildifier
    go get -u github.com/3rf/codecoroner
    gometalinter --install >/dev/null
    bin/bazel_to_go.py
}

run_linters() {
    buildifier -showlog -mode=check $(find . -name BUILD -type f)

    # TODO: Enable this once more of mixer is connected and we don't
    # have dead code on purpose
    # codecoroner funcs ./...
    # codecoroner idents ./...

    gometalinter --deadline=300s --disable-all\
        --enable=aligncheck\
        --enable=deadcode\
        --enable=errcheck\
        --enable=gas\
        --enable=goconst\
        --enable=gofmt\
        --enable=goimports\
        --enable=golint --min-confidence=0 --exclude=.pb.go --exclude="should have a package comment" --exclude="adapter.AdapterConfig"\
        --enable=gosimple\
        --enable=ineffassign\
        --enable=interfacer\
        --enable=lll --line-length=160\
        --enable=misspell\
        --enable=staticcheck\
        --enable=structcheck\
        --enable=unconvert\
        --enable=unused\
        --enable=varcheck\
        --enable=vetshadow\
        ./...

    # TODO: These generate warnings which we should fix, and then should enable the linters
    # --enable=dupl\
    # --enable=gocyclo\
    #
    # This doesn't work with our source tree for some reason, it can't find vendored imports
    # --enable=gotype\
}

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

echo Preparing linters
prep_linters

echo Running linters
run_linters

echo Done running linters
