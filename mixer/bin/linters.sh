#!/bin/bash

# Runs all requisite linters over the whole mixer code base.
set -e

prep_linters() {
	if ! which codecoroner > /dev/null; then
		echo "Preparing linters"
		go get -u github.com/alecthomas/gometalinter
		go get -u github.com/bazelbuild/buildifier/buildifier
		go get -u github.com/3rf/codecoroner
		gometalinter --install --vendored-linters >/dev/null
	fi
    bin/bazel_to_go.py
}

go_metalinter() {
    if [[ ! -z ${TRAVIS_PULL_REQUEST} ]];then
	# if travis pull request only lint changed code.
	if [[ ${TRAVIS_PULL_REQUEST} != "false" ]]; then
	    LAST_GOOD_GITSHA=${TRAVIS_COMMIT_RANGE}
	fi
    else
        # for local run, only lint the current branch
	LAST_GOOD_GITSHA=$(git log master.. --pretty="%H"|tail -1)
    fi

    # default: lint everything. This runs on the main build
    PKGS="./pkg/... ./cmd/... ./adapter/..."

    # convert LAST_GOOD_GITSHA to list of packages.
    if [[ ! -z ${LAST_GOOD_GITSHA} ]];then
        PKGS=$(for fn in $(git diff --name-only ${LAST_GOOD_GITSHA}); do fd="${fn%/*}"; [ -d ${fd} ] && echo $fd; done | sort | uniq)
    fi

    gometalinter\
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
        --enable=golint --min-confidence=0 --exclude=.pb.go --exclude="should have a package comment"\
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
        --enable=vet\
        --enable=vetshadow\
	$PKGS

    # TODO: These generate warnings which we should fix, and then should enable the linters
    # --enable=dupl\
    # --enable=gocyclo\
    #
    # This doesn't work with our source tree for some reason, it can't find vendored imports
    # --enable=gotype\
}

run_linters() {
    echo Running linters
    buildifier -showlog -mode=check $(find . -name BUILD -type f)
    go_metalinter
    $SCRIPTPATH/check_license.sh
    $SCRIPTPATH/check_workspace.sh

    # TODO: Enable this once more of mixer is connected and we don't
    # have dead code on purpose
    # codecoroner funcs ./...
    # codecoroner idents ./...
}

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

prep_linters

run_linters

echo Done running linters
