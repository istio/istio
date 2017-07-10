#!/bin/bash

set -ex

if [ "$CI" == "bootstrap" ]; then
    # ensure correct path
    mkdir -p $GOPATH/src/istio.io
    mv $GOPATH/src/github.com/istio/test-infra $GOPATH/src/istio.io
    cd $GOPATH/src/istio.io/test-infra/
fi

echo 'Running Linters'
./bin/linters.sh

echo 'Running Unit Tests'
bazel test //...

echo 'Running Integration Tests'
./tests/e2e.sh --test_logs_path='_artifacts'

