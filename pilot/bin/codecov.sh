#!/usr/bin/env bash

set -o errexit
set -o nounset
set -o pipefail

echo "" > coverage.txt

for d in $(go list ./... | grep -v vendor); do
    options="-coverprofile=profile.out"
    go test $options $d

    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done
