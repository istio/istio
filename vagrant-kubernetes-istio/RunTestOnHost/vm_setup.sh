#!/bin/bash

# Setup vagrant.
echo "Setup vagrant"
vagrant up --provider virtualbox
vagrant ssh -c "echo export HUB=10.10.0.2:5000 >> ~/.bashrc"
vagrant ssh -c "echo export TAG=latest >> ~/.bashrc"
vagrant ssh -c "echo export GOPATH=/home/vagrant/go >> ~/.bashrc"
vagrant ssh -c "echo export PATH=$PATH:/usr/local/go/bin:/go/bin:/home/vagrant/go/bin >> ~/.bashrc"
vagrant ssh -c "source ~/.bashrc"

#Setup delve on vagrant
vagrant ssh -c "/usr/local/go/bin/go get github.com/derekparker/delve/cmd/dlv"

#Create symbolic link between shared istio folder source at GOPATH, so that
# delve can find it while debugging.
vagrant ssh -c "sudo ln -s /go /home/vagrant/go"

# Adding insecure registry on VM.
echo "Adding insecure registry to docker daemon in vagrant vm..."
vagrant ssh -c "sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service"
vagrant ssh -c "sudo systemctl daemon-reload"
vagrant ssh -c "sudo systemctl restart docker"

# Setting up kubernetest Cluster on VM for Istio Tests.
echo "Adding priviledges to kubernetes cluster..."
vagrant ssh -c "sudo sed -i 's/ExecStart=\/usr\/bin\/hyperkube kubelet/ExecStart=\/usr\/bin\/hyperkube kubelet --allow-privileged=true/' /etc/systemd/system/kubelet.service"
vagrant ssh -c "sudo systemctl daemon-reload"
vagrant ssh -c "sudo systemctl stop kubelet"
vagrant ssh -c "sudo systemctl restart kubelet.service"
vagrant ssh -c "sudo sed -i 's/ExecStart=\/usr\/bin\/hyperkube apiserver/ExecStart=\/usr\/bin\/hyperkube apiserver --allow-privileged=true/' /etc/systemd/system/kube-apiserver.service"
vagrant ssh -c "sudo sed -i 's/--admission-control=AlwaysAdmit,ServiceAccount/--admission-control=AlwaysAdmit,NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota/'  /etc/systemd/system/kube-apiserver.service"
vagrant ssh -c "sudo systemctl daemon-reload"
vagrant ssh -c "sudo systemctl stop kube-apiserver"
vagrant ssh -c "sudo systemctl restart kube-apiserver"
vagrant reload
vagrant ssh -c "kubectl get pods -n kube-system"

