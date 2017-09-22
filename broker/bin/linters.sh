#!/bin/bash
set -x

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
$SCRIPTPATH/install_linters.sh
$SCRIPTPATH/prepare_linter.sh

buildifier -showlog -mode=check $(find . -type f \( -name 'BUILD' -or -name 'WORKSPACE' -or -wholename '.*bazel$' -or -wholename '.*bzl$' \) -print )

NUM_CPU=$(getconf _NPROCESSORS_ONLN)

echo "Start running linters"
gometalinter --concurrency=${NUM_CPU} --enable-gc --deadline=300s --disable-all\
  --enable=aligncheck\
  --enable=deadcode\
  --enable=errcheck\
  --enable=gas\
  --enable=goconst\
  --enable=gofmt\
  --enable=goimports\
  --enable=golint\
  --min-confidence=0\
  --enable=gotype\
  --enable=ineffassign\
  --enable=interfacer\
  --enable=lll --line-length=120\
  --enable=megacheck\
  --enable=misspell\
  --enable=structcheck\
  --enable=unconvert\
  --enable=varcheck\
  --enable=vet\
  --enable=vetshadow\
  --exclude=vendor\
  --exclude=.pb.go\
  --exclude=.*.gen.go\
  --exclude=.*_test.go\
  --exclude="should have a package comment"\
  ./...

# Disabled linters:
# --enable=dupl\
# --enable=gocyclo\
# --cyclo-over=15\

echo "Done running linters"

$SCRIPTPATH/check_license.sh
$SCRIPTPATH/check_workspace.sh

echo "Done checks"
