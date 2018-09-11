#!/bin/bash

# NOTE: libvirtd will be enabled when system boots.

SCRIPTPATH="$(cd "$(dirname "$0")" || exit ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_linux.sh
source "${ROOTDIR}/common_linux.sh"

# Check if yum is installed
if ! yum --help > /dev/null; then
  echo "yum not installed. Please install it manually and run this script again."
  exit 1
fi

sudo yum --quiet -y update

# Check if rpm is installed
if ! rpm --help > /dev/null; then
  echo "rpm not installed. Please install it and run this script again."
  exit 1
fi

#Install Curl
echo "Checking and Installing Curl as required"
if ! curl --help > /dev/null; then
  sudo yum -y install curl
  if ! curl --help > /dev/null; then
    echo "curl could not be installed. Please install it manually and run this script again."
    exit 1
  fi
fi

#Install Kvm2
echo "Installing KVM2 as required"
sudo modprobe kvm > /dev/null 2>&1
packages_to_install=("qemu-kvm" "qemu-img" "virt-manager" "libvirt" "libvirt-python" "libvirt-client" "libguestfs-tools" "virt-install" "virt-viewer" "bridge-utils")
check_and_install_packages yum "${packages_to_install[@]}"
sudo systemctl start libvirtd
sudo usermod -a -G libvirt "$(whoami)"
curl -LO https://storage.googleapis.com/minikube/releases/latest/docker-machine-driver-kvm2 && chmod +x docker-machine-driver-kvm2 && sudo mv docker-machine-driver-kvm2 /usr/local/bin/
# We run following commands only for making scripts resilient to failures. Hence
# ignoring any errors from them too.
sudo virsh net-autostart default > /dev/null 2>&1
sudo virsh net-start default > /dev/null 2>&1

install_kubectl

#Install Docker
echo "Checking and Installing Docker as required"
if ! docker --help > /dev/null; then
  curl -L https://download.docker.com/linux/centos/7/x86_64/stable/Packages/docker-ce-18.03.0.ce-1.el7.centos.x86_64.rpm -o docker-ce.rpm
  if ! sudo rpm -ivh docker-ce.rpm; then
    echo "Looks like docker installation failed."
    echo "Please install it manually and then run this script again."
    exit 1
  fi
fi

install_minikube

echo "Everything installed for you and you are ready to go!"
