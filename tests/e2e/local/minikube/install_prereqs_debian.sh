#!/bin/bash

source "../common_linux.sh"

check_apt_get
sudo apt-get --quiet -y update

check_dpkg

install_curl

#Install Kvm2
echo "Installing KVM2 as required"
sudo modprobe kvm > /dev/null 2>&1
sudo apt-get install -y  libvirt-bin > /dev/null 2>&1
sudo apt-get install -y libvirt-daemon-system libvirt-dev libvirt-clients virt-manager
sudo apt-get install -y qemu-kvm
sudo systemctl stop libvirtd
sudo systemctl start libvirtd
sudo usermod -a -G libvirt "$(whoami)"
curl -LO https://storage.googleapis.com/minikube/releases/latest/docker-machine-driver-kvm2 && chmod +x docker-machine-driver-kvm2 && sudo mv docker-machine-driver-kvm2 /usr/local/bin/
# We run following commands only for making scripts resilient to failures. Hence
# ignoring any errors from them too.
sudo virsh net-autostart default > /dev/null 2>&1
sudo virsh net-start default > /dev/null 2>&1

install_kubectl

install_docker

# Install minikube.
function install_minikube() {
  if ! (curl -Lo minikube https://storage.googleapis.com/minikube/releases/v0.27.0/minikube-linux-amd64 && \ 
      chmod +x minikube && \
      sudo mv minikube /usr/local/bin/); then
      echo "Looks like minikube installation failed."
      echo "Please install it manually and then run this script again."
      exit 1
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
    rm -rf ~/.minikube

    install_minikube
  fi
else
  install_minikube
fi

echo "Everything installed for you and you are ready to go!"
