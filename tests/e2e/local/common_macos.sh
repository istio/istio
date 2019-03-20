#!/bin/bash

# Copyright 2018 Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at

#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Common utilities for Istio local installation on MacOS platform.

set -o nounset
set -o pipefail

# Install Curl on MacOS platform
function install_curl() {
  echo "Checking curl"
  if ! curl --help > /dev/null; then
    echo "curl is not installed. Install it from homebrew."
    if ! brew install curl; then
      echo "Installation of curl from brew fails. Please install it manually."
      exit 1
    else
      echo "Done."
    fi
  else
    echo "curl exists."
  fi
}

# Install Docker
function install_docker() {
  echo "Checking docker..."
  if ! docker --help > /dev/null; then
    echo "docker is not installed. Install it from homebrew cask."
    if ! brew cask install docker; then
      echo "Installation of docker from brew fails. Please install it manually."
      exit 1
    else
      echo "Done."
    fi
  else
    echo "docker exists. Please make sure to update it to latest version."
  fi
}

function install_docker_machine() {
  echo "Checking docker-machine..."
  if ! docker-machine --help > /dev/null; then
    echo "docker-machine is not installed. Downloading and Installing it using curl."
    base=https://github.com/docker/machine/releases/download/v0.14.0 &&
    if ! curl -L "$base/docker-machine-$(uname -s)-$(uname -m)" >/usr/local/bin/docker-machine && chmod +x /usr/local/bin/docker-machine; then
        echo "Installation of docker-machine failed. Please install it manually."
        exit 1
    else
        echo "Done."
    fi
  else
    echo "docker-machine exists. Please make sure to update it to latest version."
    docker-machine version
  fi
}

# Install Kubectl on MacOS platform
function install_kubectl() {
  echo "Checking kubectl..."
  if ! kubectl --help > /dev/null; then
    echo "kubectl is not installed. Installing the lastest stable release..."
    if ! brew install kubectl; then
    	echo "Installation of kubectl from brew fails. Please install it manually."
        exit 1
    else
    	echo "Done."
    fi
  else
    echo "kubectl exists. Please make sure to update it to latest version."
  fi
}

# Check if homebrew is installed
function check_homebrew() {
  if ! brew --help > /dev/null; then
    echo "Homebrew is not installed. Please go to https://docs.brew.sh/Installation to install Homebrew."
    exit 1
  fi
}
