#!/bin/bash

set -ex

E2E_ARGS=()

if [ "${CI}" == "bootstrap" ]; then
    # ensure correct path
    mkdir -p ${GOPATH}/src/istio.io
    ln -s ${GOPATH}/src/github.com/istio/istio ${GOPATH}/src/istio.io
    cd ${GOPATH}/src/istio.io/istio/
    ARTIFACTS_DIR="${GOPATH}/src/istio.io/istio/_artifacts"
    E2E_ARGS+=(--test_log_path="${ARTIFACTS_DIR}")

    # use volume mount from istio-presubmit job's pod spec
    mkdir -p ${HOME}/.kube
    ln -s /etc/e2e-testing-kubeconfig/e2e-testing-kubeconfig ${HOME}/.kube/config
fi

echo 'Running Linters'
./bin/linters.sh

echo 'Running Unit Tests'
bazel test //...

echo 'Running Integration Tests'
./tests/e2e.sh ${E2E_ARGS[@]}

