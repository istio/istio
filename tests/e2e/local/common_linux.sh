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

# Common utilities for Istio local installation on linux platform.

set -o errexit
set -o nounset
set -o pipefail

# Install Curl on Ubuntu platform
function install_curl() {
  echo "Checking and Installing Curl as required"
  if ! curl --help > /dev/null; then
    sudo sed -i -e 's/us.archive.ubuntu.com/archive.ubuntu.com/g' /etc/apt/sources.list
    sudo apt-get --quiet -y install curl
    if ! curl --help > /dev/null; then
      echo "curl could not be installed. Please install it and run this script again."
      exit 1
    fi
  fi
}

# Check if dpkg is installed
function check_dpkg() {
  if ! dpkg --help > /dev/null; then
    echo "dpkg not installed. Please install it and run this script again."
    exit 1
  fi
}

# Check if apt-get is installed
function check_apt_get() {
  if ! apt-get --help > /dev/null; then
    echo "apt-get not installed. Please install it and run this script again."
    exit 1
  fi
}

# Install Docker
function install_docker() {
  echo "Checking and Installing Docker as required"
  if ! docker --help > /dev/null; then
    # docker-ce depends on libltdl7
    apt-get install -y libltdl7
    curl -L https://download.docker.com/linux/debian/dists/stretch/pool/stable/amd64/docker-ce_18.03.0~ce-0~debian_amd64.deb -o docker-ce.deb
    if ! sudo dpkg -i docker-ce.deb; then
      echo "Looks like docker installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
    fi
  fi
}

# Install Kubectl on Linux platform
function install_kubectl() {
  echo "Checking and Installing Kubectl as required"
  if ! kubectl --help > /dev/null; then
    curl -LO https://storage.googleapis.com/kubernetes-release/release/"$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)"/bin/linux/amd64/kubectl
    chmod +x ./kubectl
    sudo mv ./kubectl /usr/local/bin/kubectl
  fi
}

# check_and_install_packages checks if a package is installed before installing it.
# It expects 2 parameters:
# $1 - apt|yum  (supported package management tools).
# $2 - list of packages to be checked or installed.
function check_and_install_packages() {
  if [ "$#" -ne 2 ]; then
    echo "Arguments are not equals to 2"
    echo "Usage: apt|yum package_list"
    exit 1
  fi
  check_cmd="$1"
  arr="$2"
  for i in "${arr[@]}";
  do
    case $check_cmd in
      apt)
        if ! apt list --installed "$i" | grep installed > /dev/null 2>&1; then
          sudo apt-get install -y "$i" > /dev/null 2>&1
        fi;;
      yum)
        if ! yum -q list installed "$i" > /dev/null 2>&1; then
           yum install -y "$i" > /dev/null 2>&1
        fi;;
      *)
        echo "unsupported package management tool";;
    esac
  done
}
