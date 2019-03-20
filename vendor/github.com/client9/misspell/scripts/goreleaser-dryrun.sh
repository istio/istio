#!/bin/sh
echo "Real publishing is done by travis-ci"

set -ex
echo "TAG:= $(git tag | tail -1)"
rm -rf ./dist
goreleaser --skip-publish --skip-validate
