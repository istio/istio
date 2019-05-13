#!/bin/bash

# Copyright 2019 Istio Authors
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

function install_golangcilint() {
    # if you want to update this version, also change the version number in .golangci.yml
    GOLANGCI_VERSION="v1.16.0"
    curl -sfL https://install.goreleaser.com/github.com/golangci/golangci-lint.sh | sh -s -- -b "$GOPATH"/bin "$GOLANGCI_VERSION"
    golangci-lint --version
}

function run_golangcilint() {
    echo 'Running golangci-lint ...'
    env GOGC=25 golangci-lint run -j 1 -v ./...
}

install_golangcilint
run_golangcilint
