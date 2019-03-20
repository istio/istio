#!/bin/sh
set -ex

# DEP
curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh

# GOMETALINTER
go get -u github.com/alecthomas/gometalinter && gometalinter --install

# remove the default misspell to make sure
rm -f `which misspell`
