#!/bin/bash

set -ex

if [ "${CI}" == "bootstrap" ]; then
    # ensure correct path
    mkdir -p ${GOPATH}/src/istio.io
    mv ${GOPATH}/src/github.com/istio/istio ${GOPATH}/src/istio.io
    cd ${GOPATH}/src/istio.io/istio/

    # use volume mount from istio-presubmit job's pod spec
    ln -s /etc/e2e-testing-kubeconfig/e2e-testing-kubeconfig ${HOME}/.kube/config
fi

echo 'Running Linters'
./bin/linters.sh

echo 'Running Unit Tests'
bazel test //...

echo 'Running Integration Tests'
./tests/e2e.sh --test_log_path=_artifacts

