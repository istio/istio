#!/bin/bash

source ../common_linux.sh

check_apt_get
sudo apt-get --quiet -y update

check_dpkg
install_curl

# Install virtualbox.
echo "Checking and Installing Virtualbox as required"
if ! virtualbox --help > /dev/null; then
    curl -L https://download.virtualbox.org/virtualbox/5.2.10/virtualbox-5.2_5.2.10-122088~Ubuntu~trusty_amd64.deb --output virtualbox.deb
    if ! sudo dpkg -i virtualbox.deb; then
      echo "Looks like virtual box installation failed. It could be that it's missing some sub-packages. "
      echo "Please install those packages and then run this script again."
      exit 1
    fi
    sudo apt-get --quite -y install -f
    # Check for more recent version and update
    if sudo apt-get --quite -y install virtualbox; then
      echo "virtual box install done! Current Version: $(VBoxManage -v)"
    else
      echo "Looks like virtual box update failed. Please try manually. Current Version: $(VBoxManage -v)"
      exit 1
    fi
else
    echo "Looks like virtual is installed. Checking if it can be upgraded."
    if ! sudo apt-get --quite -y install virtualbox; then
      echo "Looks like virtual box update failed. Please try manually. Current Version: $(VBoxManage -v)"
      exit 1
    else
      echo "virtual box install done! Current Version: $(VBoxManage -v)"
    fi
fi

install_docker

# Install vagrant.
echo "Checking and Installing Vagrant as required"
if ! vagrant --help > /dev/null; then
  sudo apt-get --quiet -y update
  if ! sudo apt-get --quiet -y install vagrant; then
      echo "Looks like vagrant installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

install_kubectl

echo "Everything installed for you and you are ready to go!"


