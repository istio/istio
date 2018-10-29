#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" || exit ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_linux.sh
source "${ROOTDIR}/common_linux.sh"

check_yum
sudo yum --quiet -y update

check_rpm

#Install Curl
echo "Checking and Installing Curl as required"
if ! curl --help > /dev/null; then
  sudo yum -y install curl
  if ! curl --help > /dev/null; then
    echo "curl could not be installed. Please install it manually and run this script again."
    exit 1
  fi
fi

# Install virtualbox.
echo "Checking and Installing Virtualbox as required"
if ! VBoxManage -v > /dev/null; then
  if ! sudo yum --quiet -y install virtualbox; then
      echo "Looks like virtualbox installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

install_docker

# Install vagrant.
echo "Checking and Installing Vagrant as required"
if ! vagrant --help > /dev/null; then
  if ! sudo yum --quiet -y install vagrant; then
      echo "Looks like vagrant installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

install_kubectl

echo "Everything installed for you and you are ready to go!"
