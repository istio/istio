#!/bin/bash

# Setup vagrant.
echo "Setup vagrant"
vagrant up --provider virtualbox
vagrant ssh -c "echo export HUB=10.10.0.2:5000 >> ~/.bashrc"
vagrant ssh -c "echo export TAG=latest >> ~/.bashrc"
vagrant ssh -c "echo export GOPATH=/home/vagrant/go >> ~/.bashrc"
vagrant ssh -c "echo export PATH=$PATH:/usr/local/go/bin:/go/bin:/home/vagrant/go/bin >> ~/.bashrc"
vagrant ssh -c "echo export ISTIO=/home/vagrant/go/src/istio.io >> ~/.bashrc"
vagrant ssh -c "source ~/.bashrc"

#Setup delve on vagrant
vagrant ssh -c "/usr/local/go/bin/go get github.com/derekparker/delve/cmd/dlv"

#Setup Istio Directory.
vagrant ssh -c "mkdir -p /home/vagrant/go/src/istio.io"
# We cannot directly set up synced folder between $ISTIO in host machine and $ISTIO in VM.
# Because at VM boot up stage synced folder setup comes before privision bootstrap.sh.
# Therefore directory $ISTIO in VM does not exist when Vagrant sets up synced folder.
# We synced $ISTIO from host to /istio.io in VM, and create a softlink between /istio.io/istio and $ISTIO/istio.
vagrant ssh -c "sudo ln -s /istio.io/istio/ /home/vagrant/go/src/istio.io/istio"

# Adding insecure registry on VM.
echo "Adding insecure registry to docker daemon in vagrant vm..."
vagrant ssh -c "sudo sed -i 's/ExecStart=\\/usr\\/bin\\/dockerd -H fd:\\/\\//ExecStart=\\/usr\\/bin\\/dockerd -H fd:\\/\\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service"
vagrant ssh -c "sudo systemctl daemon-reload"
vagrant ssh -c "sudo systemctl restart docker"

# Setting up kubernetest Cluster on VM for Istio Tests.
echo "Adding priviledges to kubernetes cluster..."
vagrant ssh -c "sudo sed -i 's/ExecStart=\\/usr\\/bin\\/hyperkube kubelet/ExecStart=\\/usr\\/bin\\/hyperkube kubelet --allow-privileged=true/' /etc/systemd/system/kubelet.service"
vagrant ssh -c "sudo systemctl daemon-reload"
vagrant ssh -c "sudo systemctl stop kubelet"
vagrant ssh -c "sudo systemctl restart kubelet.service"
vagrant ssh -c "sudo sed -i 's/ExecStart=\\/usr\\/bin\\/hyperkube apiserver/ExecStart=\\/usr\\/bin\\/hyperkube apiserver --allow-privileged=true/' /etc/systemd/system/kube-apiserver.service"
vagrant ssh -c "sudo sed -i 's/--admission-control=AlwaysAdmit,ServiceAccount/--admission-control=AlwaysAdmit,NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota/'  /etc/systemd/system/kube-apiserver.service"
vagrant ssh -c "sudo systemctl daemon-reload"
vagrant ssh -c "sudo systemctl stop kube-apiserver"
vagrant ssh -c "sudo systemctl restart kube-apiserver"
echo "$(tput setaf 1)Make sure flag --allow-privileged=true is passed to both kubelet and apiserver.$(tput sgr 0)"
# shellcheck disable=SC2009
ps -ef | grep kube
vagrant reload
vagrant ssh -c "kubectl get pods -n kube-system"
vagrant ssh -c "mkdir -p ~/.kube/"
vagrant ssh -c "cp /etc/kubeconfig.yml ~/.kube/config"

echo "Setup done."
