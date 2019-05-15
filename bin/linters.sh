#!/bin/bash

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

set -ex

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

WORKSPACE=$SCRIPTPATH/..

cd "${WORKSPACE}"

if [[ -z $SKIP_INIT ]];then
  bin/init.sh
fi

function ensure_pilot_types() {
    echo 'Checking Pilot types generation ....'
    bin/check_pilot_codegen.sh
    echo 'Pilot types generation OK'
}

function check_licenses() {
    echo 'Checking licenses'
    bin/check_license.sh
    echo 'licenses OK'
}

function install_golangcilint() {
    # if you want to update this version, also change the version number in .golangci.yml
    GOLANGCI_VERSION="v1.16.0"
    curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b "$GOPATH"/bin "$GOLANGCI_VERSION"
    golangci-lint --version
}

function run_adapter_lint() {
    echo 'Running adapterlinter ....'
    go build -o bin/adapterlinter mixer/tools/adapterlinter/main.go
    bin/adapterlinter ./mixer/adapter/...
    echo 'adapterlinter OK'
}

function run_test_lint() {
    echo 'Running testlinter ...'
    go build -o bin/testlinter tools/checker/testlinter/*.go
    bin/testlinter
    echo 'testlinter OK'
}

function run_envvar_lint() {
    echo 'Running envvarlinter ...'
    go build -o bin/envvarlinter tools/checker/envvarlinter/*.go
    bin/envvarlinter mixer pilot security galley istioctl
    echo 'envvarlinter OK'
}

function run_golangcilint() {
    echo 'Running golangci-lint ...'
    env GOGC=25 golangci-lint run -j 1 -v ./...
}

function run_helm_lint() {
    echo 'Running helm lint on istio ....'
    helm lint ./install/kubernetes/helm/istio
    echo 'helm lint on istio OK'
}

function check_grafana_dashboards() {
    echo 'Checking Grafana dashboards'
    bin/check_dashboards.sh
    echo 'dashboards OK'
}

function check_licenses() {
    echo 'Checking Licenses for Istio dependencies'
    go run tools/license/get_dep_licenses.go > LICENSES.txt
    echo 'Licenses OK'
}

function check_samples() {
    echo 'Checking documentation samples with istioctl'
    bin/check_samples.sh
    echo 'Samples OK'
}

ensure_pilot_types
check_licenses
install_golangcilint
run_golangcilint
run_adapter_lint
run_test_lint
run_envvar_lint
run_helm_lint
check_grafana_dashboards
check_samples
