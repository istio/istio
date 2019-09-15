#!/bin/bash

# Copyright Istio Authors
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

SCRIPTPATH="$(cd "$(dirname "$0")" || exit ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_macos.sh
source "${ROOTDIR}/common_macos.sh"

check_homebrew

echo "Update homebrew..."
brew update

install_curl

install_docker

echo """
NOTE: When running kind on MacOS it is recommended that you have at least 4GB of RAM
and disk space (these are estimates for a single node kind cluster) dedicated to the
virtual machine (VM) running the Docker engine otherwise the Kubernetes cluster might
fail to start up. More info check User Guide: https://kind.sigs.k8s.io/
"""

install_kubectl

function check_and_install_golang() {
    echo "Checking Golang is installed..."
    if ! go help > /dev/null; then
        echo "Golang is not installed. Installing the lastest stable release..."
        if ! brew install golang; then
            echo "Installation of Golang from brew fails. Please install it manually."
            exit 1
        else
            echo "Done."
        fi
    else
        echo "Golang exists. Please make sure to update it to latest version."
    fi
}

function check_and_install_kind() {
    echo "Checking KinD is installed..."
    if ! kind --help > /dev/null; then
        if ! (GO111MODULE="on" go get sigs.k8s.io/kind@v0.3.0); then
            echo "Looks like KinD installation failed."
            echo "Please install it manually then run this script again."
            exit 1
        else
            echo "Done."
        fi
    else
        echo "KinD exists. Please make sure to update it to latest version."
    fi
}

check_and_install_golang
check_and_install_kind

echo "Prerequisite check and installation process finishes."
