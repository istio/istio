#!/usr/bin/env bash

set -e
echo "" > coverage.txt

for d in $(go list ./... | grep -v vendor); do
    options="-coverprofile=profile.out -covermode=atomic"
    case $d in
        istio.io/pilot/adapter/config/tpr)
            echo "Skipping race detection on $d (see https://github.com/istio/pilot/issues/173)"
            ;;
        *)
            options=$options" -race"
            ;;
    esac
    go test $options $d

    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done
