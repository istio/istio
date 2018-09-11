#!/bin/bash

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_linux.sh
source "${ROOTDIR}/common_linux.sh"

check_apt_get
sudo apt-get --quiet -y update

check_dpkg
install_curl

# Install virtualbox.
echo "Checking and Installing Virtualbox as required"
if ! VBoxManage -v > /dev/null; then
  if ! sudo apt-get --quiet -y install virtualbox; then
      echo "Looks like virtualbox installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

install_docker

# Install vagrant.
echo "Checking and Installing Vagrant as required"
if ! vagrant --help > /dev/null; then
  if ! sudo apt-get --quiet -y install vagrant; then
      echo "Looks like vagrant installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

install_kubectl

echo "Everything installed for you and you are ready to go!"


