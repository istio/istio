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
brew update > /dev/null
# Give write permissions for /usr/local/bin, so that brew can create symlinks.
sudo chown -R "$USER:admin" /usr/local/bin

install_curl

install_docker

install_docker_machine

function fail_hyperkit_installation() {
    # shellcheck disable=SC2181
    if [ $? -ne 0 ]; then
        echo "Installation of hyperkit driver failed. Please install it manually."
        exit 1
    fi
}

echo "Checking hyperkit..."
if ! hyperkit -h > /dev/null; then
    echo "hyperkit is not installed. Downloading and installing using curl."
    curl -LO https://storage.googleapis.com/minikube/releases/latest/docker-machine-driver-hyperkit
    fail_hyperkit_installation
    chmod +x docker-machine-driver-hyperkit
    fail_hyperkit_installation
    sudo mv docker-machine-driver-hyperkit /usr/local/bin/
    fail_hyperkit_installation
    sudo chown root:wheel /usr/local/bin/docker-machine-driver-hyperkit
    fail_hyperkit_installation
    sudo chmod u+s /usr/local/bin/docker-machine-driver-hyperkit
    fail_hyperkit_installation
else
    echo "hyperkit is installed. Install newer version if available."
    hyperkit version
fi

# Install minikube.
function install_minikube() {
    echo "Minikube version 0.27.0 is not installed. Installing it using curl."
    if ! curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.27.0/minikube-darwin-amd64 \
            && chmod +x minikube \
            && sudo mv minikube /usr/local/bin/; then
        echo "Installation of Minikube version 0.27.0 failed. Please install it manually."
        exit 1
    else
        echo "Done."
    fi
}

echo "Checking and Installing Minikube version 0.27.0 as required"

# If minikube is installed.
if minikube --help > /dev/null; then
# If version is not 0.27.0.
if [[ $(minikube version) != *"minikube version: v0.27.0"* ]]; then
    # Uninstall minikube.
    echo "Deleting previous minikube cluster and updating minikube to v0.27.0"
    minikube delete

    install_minikube
fi
else
    install_minikube
fi

install_kubectl

echo "Prerequisite check and installation process finishes."
