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
    *) { echo "Unrecognized argument ${OPTARG}"; exit 1; };;
  esac
done

prep_linters() {
    bin/install_linters.sh
    bin/bazel_to_go.py
}

go_metalinter() {
    local parent_branch="${PARENT_BRANCH}"
    echo parent_branch = "${parent_branch}"
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
        PKGS="./adapter/... ./cmd/... ./example/... ./pkg/... ./tools/codegen/..."

        # convert LAST_GOOD_GITSHA to list of packages.
        if [[ ! -z ${LAST_GOOD_GITSHA} ]];then
            echo "Using ${LAST_GOOD_GITSHA} to compare files to."
            list=""
            for fn in $(git diff --name-only ${LAST_GOOD_GITSHA}); do
                # Always skip testdata and bin
                case ${fn} in
                    testdata/* | bin/*) : ;;
                    *) { fd="${fn%/*}"; if [[ -d ${fd} ]]; then list="${list}\n${fd}"; fi; } ;;
                esac
            done
            PKGS=$(echo -e "${list}" | sort | uniq)
            echo -e "Running linters on: ${PKGS}\n"
        else
            echo "Running linters on all files."
        fi
    fi

    # Note: WriteHeaderAndJson excluded because the interface is defined in a 3rd party library.
    gometalinter.v1\
        --concurrency=4\
        --enable-gc\
        --vendored-linters\
        --deadline=1200s --disable-all\
        --enable=gosimple\
        --enable=aligncheck\
        --enable=deadcode\
        --enable=errcheck\
        --enable=gas\
        --enable=goconst\
        --enable=gofmt\
        --enable=goimports\
        --enable=golint --min-confidence=0 --exclude=vendor --exclude=.pb.go --exclude=pkg/config/proto/combined.go --exclude=.*.gen.go --exclude="should have a package comment"\
        --exclude=".*pkg/config/apiserver_test.go:.* method WriteHeaderAndJson should be WriteHeaderAndJSON"\
        --enable=ineffassign\
        --enable=interfacer\
        --enable=lll --line-length=160\
        --enable=misspell\
        --enable=staticcheck\
        --enable=structcheck\
        --enable=unconvert\
        --enable=unparam\
        --enable=unused\
        --enable=varcheck\
        --enable=vet\
        --enable=vetshadow\
        --skip=testdata\
        --skip=vendor\
        --vendor\
        $PKGS
}

run_linters() {
    echo Running linters
    buildifier -showlog -mode=check $(find . -name BUILD -type f)
    buildifier -showlog -mode=check $(find . -name BUILD.bazel -type f)
    buildifier -showlog -mode=check ./BUILD.ubuntu
    buildifier -showlog -mode=check ./WORKSPACE
    go_metalinter
    $SCRIPTPATH/check_license.sh
    $SCRIPTPATH/check_workspace.sh
}

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR

prep_linters
run_linters

echo Done running linters
