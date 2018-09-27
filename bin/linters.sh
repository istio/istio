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

function format() {
    echo 'Running format/imports check ....'
    bin/fmt.sh -c
    echo 'Format/imports check OK'
}

function check_licenses() {
    echo 'Checking licenses'
    bin/check_license.sh
    echo 'licenses OK'
}

function check_spelling() {
    echo 'Checking spelling'
    bin/check_spelling.sh
    echo 'spelling OK'
}

function check_boilerplate() {
    echo 'Checking boilerplate'
    bin/check_boilerplate.sh
    echo 'boilerplate check OK'
}

function has_latest_gometalinter() {
    local local_binary
    local lastest_version
    local current_version

    local_binary="${1}"
    lastest_version="${2}"
    current_version="$(${local_binary} --version 2>/dev/null | cut -d ' ' -f 3)"

    if [ "${lastest_version}" != "${current_version}" ]; then
        return 1
    fi

    return 0
}

function install_gometalinter() {
    gometalinter=$(command -v gometalinter 2> /dev/null || echo "${ISTIO_BIN}/gometalinter")
    latest_version=$(curl -L -s https://api.github.com/repos/alecthomas/gometalinter/releases/latest \
	    | grep tag_name | sed "s/ *\"tag_name\": *\"\\(.*\\)\",*/\\1/" | sed "s/v//")

    if has_latest_gometalinter "${gometalinter}" "${latest_version}"; then
        echo "Skipping gometalinter installation, we already have the latest version"
        return 0
    fi

    echo 'Installing gometalinter ....'
    curl -s "https://raw.githubusercontent.com/alecthomas/gometalinter/v${latest_version}/scripts/install.sh" | bash -s -- -b "${ISTIO_BIN}"
    if [ ! -x "${ISTIO_BIN}/gometalinter" ]; then
        echo "Installation of gometalinter failed"
        exit 1
    fi

    echo 'Gometalinter installed successfully'
}

function run_gometalinter() {
    echo 'Running gometalinter ....'
    $gometalinter --config=./lintconfig_base.json ./...
    echo 'gometalinter OK'
    echo 'Running gometalinter on adapters ....'
    pushd mixer/tools/adapterlinter
    go install .
    popd

    $gometalinter --config=./mixer/tools/adapterlinter/gometalinter.json ./mixer/adapter/...
    echo 'gometalinter on adapters OK'

    echo 'Running testlinter ...'
    pushd tests/util/checker/testlinter
    go install .
    popd
    $gometalinter --config=./tests/util/checker/testlinter/testlinter.json ./...
    echo 'testlinter OK'
}

function run_helm_lint() {
    echo 'Running helm lint on istio & istio-remote ....'
    helm lint ./install/kubernetes/helm/{istio,istio-remote}
    echo 'helm lint on istio & istio-remote OK'
}

function check_grafana_dashboards() {
    echo 'Checking Grafana dashboards'
    bin/check_dashboards.sh
    echo 'dashboards OK'
}

ensure_pilot_types
format
check_licenses
check_boilerplate
check_spelling
install_gometalinter
run_gometalinter
run_helm_lint
check_grafana_dashboards