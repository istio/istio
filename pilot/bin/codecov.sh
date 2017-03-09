#!/usr/bin/env bash

set -e
echo "" > coverage.txt

for d in $(go list ./... | grep -v vendor); do
    options="-coverprofile=profile.out -covermode=atomic"
    case $d in
        istio.io/manager/platform/kube)
            echo "Skipping race detection on $d (see https://github.com/istio/manager/issues/173)"
            ;;
        *)
            options=$options" -race"
            ;;
    esac
    go generate $d
    go test $options $d

    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done
