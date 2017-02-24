#!/bin/bash
set -ex

# Create symlinks from bazel directory to the vendor folder.
bin/bazel_to_go.py

# Remove nested vendor/ directories.
find -L vendor/ -mindepth 1 -name vendor | xargs rm -rf

go install ./...

gometalinter --concurrency=4 --enable-gc --deadline=300s --disable-all\
  --enable=aligncheck\
  --enable=deadcode\
  --enable=errcheck\
  --enable=gas\
  --enable=goconst\
  --enable=gofmt\
  --enable=goimports\
  --enable=golint --exclude=.pb.go\
  --enable=gosimple\
  --enable=gotype\
  --enable=ineffassign\
  --enable=interfacer\
  --enable=lll --line-length=120\
  --enable=misspell\
  --enable=staticcheck\
  --enable=structcheck\
  --enable=unconvert\
  --enable=unused\
  --enable=varcheck\
  --enable=vet\
  --enable=vetshadow\
  ./...
