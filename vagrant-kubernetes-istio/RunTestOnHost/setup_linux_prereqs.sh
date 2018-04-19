#!/bin/bash

#Install Curl
echo "Install Curl"
sudo sed -i -e 's/us.archive.ubuntu.com/archive.ubuntu.com/g' /etc/apt/sources.list
sudo apt-get --quiet -y update
sudo apt-get --quiet -y install curl

curl -L http://mirrors.kernel.org/ubuntu/pool/main/libp/libpng/libpng12-0_1.2.54-1ubuntu1_amd64.deb --output libpng12.deb
sudo dpkg -i libpng12.deb

curl -L http://security.debian.org/debian-security/pool/updates/main/o/openssl/libssl1.0.0_1.0.1t-1+deb8u8_amd64.deb --output libssl1.0.0.deb
sudo dpkg -i libssl1.0.0.de

curl -L http://mirrors.kernel.org/ubuntu/pool/main/libv/libvpx/libvpx3_1.5.0-2ubuntu1_amd64.deb --output libvpx3.deb
sudo dpkg -i libvpx3.deb

curl -L http://mirrors.kernel.org/ubuntu/pool/main/libs/libsdl1.2/libsdl1.2debian_1.2.15+dfsg1-3_amd64.deb --output libsdl1.2debian.deb
sudo dpkg -i libsdl1.2debian.deb

sudo apt-get install --quiet -y libpng16-16 libssl1.0.2 libvpx4

# Install virtualbox.
echo "Install virtualbox"
curl -L https://download.virtualbox.org/virtualbox/5.2.8/virtualbox-5.2_5.2.8-121009~Ubuntu~xenial_amd64.deb --output virtualbox.deb
sudo dpkg -i virtualbox.deb
sudo apt-get install -f

#Install Docker
echo "Install Docker"
curl -L https://download.docker.com/linux/debian/dists/stretch/pool/stable/amd64/docker-ce_18.03.0~ce-0~debian_amd64.deb docker-ce.deb
sudo dpkg -i docker-ce.deb

# Install vagrant.
echo "Install vagrant"
sudo apt-get --quiet -y update
sudo apt-get --quiet -y install vagrant 

# Install kubectl
echo "Install kubectl"
curl -LO https://storage.googleapis.com/kubernetes-release/release/$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)/bin/linux/amd64/kubectl
chmod +x ./kubectl
sudo mv ./kubectl /usr/local/bin/kubectl



