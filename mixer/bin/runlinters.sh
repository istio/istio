#!/bin/bash

set -ev

buildifier -showlog -mode=check $(find . -name BUILD -type f)

# TODO: Enable this once more of mixer is connected and we don't
# have dead code on purpose
# codecoroner funcs ./...
# codecoroner idents ./...

gometalinter --deadline=30s --disable-all\
	--enable=deadcode\
	--enable=gas\
	--enable=goconst\
	--enable=gofmt\
	--enable=gosimple\
	--enable=ineffassign\
	--enable=lll --line-length=160\
	--enable=misspell\
	--enable=staticcheck\
	--enable=vetshadow\
	./...

# These are disabled because we're having issues with the adapter/denyChecker package and it's
# proto. Once these issues are fixed, all of these should be enabled.
#	--enable=aligncheck\
#	--enable=errcheck\
#	--enable=interfacer\
#	--enable=structcheck\
#	--enable=unconvert\
#	--enable=unused\
#	--enable=varcheck\
#
# These generate warnings which we should fix, and then should enable the linters
# --enable=dupl
# --enable=gocyclo
# --enable=golint
#
# These don't seem interesting
# --enable=goimports
# --enable=gotype
