#!/bin/bash

set -ex

LINTER_SHA='bfcc1d6942136fd86eb6f1a6fb328de8398fbd80'

docker run \
  -v $(bazel info output_base):$(bazel info output_base) \
  -v $(pwd):/go/src/istio.io/istio/security \
  -w /go/src/istio.io/istio/security gcr.io/istio-testing/linter:${LINTER_SHA} \
  --concurrency=4 --enable-gc --deadline=300s --disable-all\
  --enable=aligncheck\
  --enable=deadcode\
  --enable=errcheck\
  --enable=gas\
  --enable=goconst\
  --enable=gofmt\
  --enable=goimports\
  --enable=golint --exclude=.pb.go\
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
  ./...
