#!/bin/bash

# Runs all requisite linters over the whole mixer code base.
set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR


PARENT_BRANCH=''

while getopts :c: arg; do
  case ${arg} in
    c) PARENT_BRANCH="${OPTARG}";;
    *) error_exit "Unrecognized argument ${OPTARG}";;
  esac
done

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
    local parent_branch="${PARENT_BRANCH}"
    if [[ ! -z ${TRAVIS_PULL_REQUEST} ]];then
        # if travis pull request only lint changed code.
        if [[ ${TRAVIS_PULL_REQUEST} != "false" ]]; then
            LAST_GOOD_GITSHA=${TRAVIS_COMMIT_RANGE}
        fi
    elif [[ ! -z ${GITHUB_PR_TARGET_BRANCH} ]]; then
        parent_branch='parent'
        git fetch origin "refs/heads/${GITHUB_PR_TARGET_BRANCH}:${parent_branch}"
    fi

    if [[ -z ${LAST_GOOD_GITSHA} ]] && [[ -n "${parent_branch}" ]]; then
        LAST_GOOD_GITSHA="$(git log ${parent_branch}.. --pretty="%H"|tail -1)"
        [[ ! -z ${LAST_GOOD_GITSHA} ]] && LAST_GOOD_GITSHA="${LAST_GOOD_GITSHA}^"
    fi

    # default: lint everything. This runs on the main build
    if [[ -z ${PKGS} ]];then
		PKGS="./pkg/... ./cmd/... ./adapter/... ./example/..."

		# convert LAST_GOOD_GITSHA to list of packages.
		if [[ ! -z ${LAST_GOOD_GITSHA} ]];then
			echo "Using ${LAST_GOOD_GITSHA} to compare files to."
			PKGS=$(for fn in $(git diff --name-only ${LAST_GOOD_GITSHA}); do fd="${fn%/*}"; [ -d ${fd} ] && echo $fd; done | sort | uniq)
		else
			echo 'Running linters on all files.'
		fi
    fi

    # Note: WriteHeaderAndJson excluded because the interface is defined in a 3rd party library.
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
        --enable=golint --min-confidence=0 --exclude=.pb.go --exclude=pkg/config/proto/combined.go --exclude="should have a package comment"\
        --exclude=".*pkg/config/apiserver_test.go:.* method WriteHeaderAndJson should be WriteHeaderAndJSON"\
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
