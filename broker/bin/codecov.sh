#!/usr/bin/env bash

set -e

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
$SCRIPTPATH/init.sh

echo "" > coverage.txt
for d in $(go list ./... | grep -v vendor); do
    options="-coverprofile=profile.out"
    go test $options $d

    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done

