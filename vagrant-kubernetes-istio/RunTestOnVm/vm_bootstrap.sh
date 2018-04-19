#!/usr/bin/env bash

sudo apt-get update
sudo apt-get --quiet -y install socat

# Install go
echo "Install go"
sudo curl -O https://dl.google.com/go/go1.10.1.linux-amd64.tar.gz
sudo tar -xf go1.10.1.linux-amd64.tar.gz
sudo mv go /usr/local

# Install git
echo "Install git"
sudo apt-get --quiet -y install git

# Setting up kubernetest Cluster on VM for Istio Tests.
echo "Adding priviledges to kubernetes cluster..."
sudo sed -i 's/ExecStart=\/usr\/bin\/hyperkube kubelet/ExecStart=\/usr\/bin\/hyperkube kubelet --allow-privileged=true/' /etc/systemd/system/kubelet.service
sudo systemctl daemon-reload
sudo systemctl stop kubelet
sudo systemctl restart kubelet.service
sudo sed -i 's/ExecStart=\/usr\/bin\/hyperkube apiserver/ExecStart=\/usr\/bin\/hyperkube apiserver --allow-privileged=true/' /etc/systemd/system/kube-apiserver.service
sudo sed -i 's/--admission-control=AlwaysAdmit,ServiceAccount/--admission-control=AlwaysAdmit,NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota/'  /etc/systemd/system/kube-apiserver.service
sudo systemctl daemon-reload
sudo systemctl stop kube-apiserver
sudo systemctl restart kube-apiserver

echo "$(tput setaf 1)Make sure flag --allow-privileged=true is passed to both kubelet and apiserver.$(tput sgr 0)"
ps -ef | grep kube

# Set up Istio
echo "export GOPATH=/home/vagrant/go" >> /home/vagrant/.profile
echo "export PATH=$PATH:$GOPATH/bin" >> /home/vagrant/.profile
echo "export PATH=$PATH:/usr/local/go/bin" >> /home/vagrant/.profile
echo "export PATH=$PATH:/home/vagrant/go/bin" >> /home/vagrant/.profile
echo "export ISTIO=/home/vagrant/go/src/istio.io" >> /home/vagrant/.profile
echo "export HUB=localhost:5000" >> /home/vagrant/.profile
echo "export TAG=latest" >> /home/vagrant/.profile
