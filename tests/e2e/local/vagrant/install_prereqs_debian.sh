#!/bin/bash

# Check if apt-get is installed
apt-get --help > /dev/null
if [ $? -ne 0 ]; then
    echo "apt-get not installed. Please install it and run this script again."
    exit 1
fi
sudo apt-get --quiet -y update

# Check if dpkg is installed
dpkg --help > /dev/null
if [ $? -ne 0 ]; then
    echo "dpkg not installed. Please install it and run this script again."
    exit 1
fi

#Install Curl
echo "Checking and Installing Curl as required"
curl --help > /dev/null
if [ $? -ne 0 ]; then
    sudo sed -i -e 's/us.archive.ubuntu.com/archive.ubuntu.com/g' /etc/apt/sources.list
    sudo apt-get --quiet -y install curl
    curl --help > /dev/null
    if [ $? -ne 0 ]; then
      echo "curl could not be installed. Please install it and run this script again."
      exit 1
    fi
fi

# Install virtualbox.
echo "Checking and Installing Virtualbox as required"
virtualbox --help > /dev/null
if [ $? -ne 0 ]; 
then
    curl -L https://download.virtualbox.org/virtualbox/5.2.10/virtualbox-5.2_5.2.10-122088~Ubuntu~trusty_amd64.deb --output virtualbox.deb
    sudo dpkg -i virtualbox.deb
    if [ $? -ne 0 ]; then
      echo "Looks like virtual box installation failed. It could be that it's missing some sub-packages. "
      echo "Please install those packages and then run this script again."
      exit 1
    fi
    sudo apt-get --quite -y install -f
    # Check for more recent version and update
    sudo apt-get --quite -y install virtualbox
    ver = 
    if [ $? -ne 0 ]; then
      echo "Looks like virtual box update failed. Please try manually. Current Version: `VBoxManage -v`"
      exit 1
    else
      echo "virtual box install done! Current Version: `VBoxManage -v`"
    fi
else
    echo "Looks like virtual is installed. Checking if it can be upgraded."
    sudo apt-get --quite -y install virtualbox
    if [ $? -ne 0 ]; then
      echo "Looks like virtual box update failed. Please try manually. Current Version: `VBoxManage -v`"
      exit 1
    else
      echo "virtual box install done! Current Version: `VBoxManage -v`"
    fi
fi


#Install Docker
echo "Checking and Installing Docker as required"
docker --help > /dev/null
if [ $? -ne 0 ]; then
  curl -L https://download.docker.com/linux/debian/dists/stretch/pool/stable/amd64/docker-ce_18.03.0~ce-0~debian_amd64.deb docker-ce.deb
  sudo dpkg -i docker-ce.deb
  if [ $? -ne 0 ]; then
      echo "Looks like docker installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

# Install vagrant.
echo "Checking and Installing Vagrant as required"
vagrant --help > /dev/null
if [ $? -ne 0 ]; then
  sudo apt-get --quiet -y update
  sudo apt-get --quiet -y install vagrant 
  if [ $? -ne 0 ]; then
      echo "Looks like vagrant installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

# Install kubectl
echo "Checking and Installing Kubectl as required"
kubectl --help > /dev/null
if [ $? -ne 0 ]; then
  curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl
  chmod +x ./kubectl
  sudo mv ./kubectl /usr/local/bin/kubectl
fi

echo "Everything installed for you and you are ready to go!"


