#!/usr/bin/env bash

set -e
echo "" > coverage.txt

for d in $(go list ./... | grep -v vendor); do
    # TODO temporarily disable race detection for initial codecov.io / github.com integration.
    # go test -race -coverprofile=profile.out -covermode=atomic $d
    go test -coverprofile=profile.out -covermode=atomic $d
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done
