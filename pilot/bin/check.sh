#!/bin/bash
set -ex

gometalinter --deadline=300s --disable-all\
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

# Disabled linters:
# --enable=dupl\
# --enable=gocyclo\
# --cyclo-over=15\
