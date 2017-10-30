#!/usr/bin/env bash
set -e
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR


echo "Code coverage test"
echo "" > coverage.txt
for d in $(go list ./... | grep -v vendor); do
    go test -coverprofile=profile.out $d
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done

