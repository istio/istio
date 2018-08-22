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

#Install Kvm2
echo "Installing KVM2 as required"
sudo apt-get install libvirt-bin
sudo apt-get install libvirt-daemon-system libvirt-dev libvirt-clients virt-manager
sudo apt-get install qemu-kvm
sudo systemctl stop libvirtd
sudo systemctl start libvirtd
sudo usermod -a -G libvirt $(whoami)
curl -LO https://storage.googleapis.com/minikube/releases/latest/docker-machine-driver-kvm2 && chmod +x docker-machine-driver-kvm2 && sudo mv docker-machine-driver-kvm2 /usr/local/bin/
# We run following commands only for making scripts resilient to failures. Hence
# ignoring any errors from them too.
sudo virsh net-autostart default > /dev/null 2>&1
sudo virsh net-start default > /dev/null 2>&1

# Install kubectl
echo "Checking and Installing Kubectl as required"
kubectl --help > /dev/null
if [ $? -ne 0 ]; then
  curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl
  chmod +x ./kubectl
  sudo mv ./kubectl /usr/local/bin/kubectl
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

# Install minikube.
echo "Checking and Installing Minikube version 0.27.0 as required"
minikube --help > /dev/null
if [[ $? -ne 0 || (`minikube version` != *"minikube version: v0.27.0"*) ]]; then
  if [ $? -eq 0]; then
    echo "Deleting previous minikube cluster and updating minikube to v0.27.0"
    minikube delete
    rm -rf ~/.minikube
  fi
  curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.27.0/minikube-linux-amd64 && chmod +x minikube && sudo mv minikube /usr/local/bin/
  if [ $? -ne 0 ]; then
      echo "Looks like minikube installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
  fi
fi

echo "Everything installed for you and you are ready to go!"
