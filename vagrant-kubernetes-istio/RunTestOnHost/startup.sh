#!/bin/bash

# Setup vagrant.
echo "Setup vagrant"
vagrant destroy
vagrant up --provider virtualbox
vagrant ssh -c "echo export HUB=10.10.0.2:5000 >> ~/.bashrc"
vagrant ssh -c "echo export TAG=latest >> ~/.bashrc"
vagrant ssh -c "source ~/.bashrc"

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

# Setting up linux host to talk to kubernetest cluster on Vagrant VM.
echo "your old ~/.kube/config file can be found at ~/.kube/config_old"
cp ~/.kube/config ~/.kube/config_old
vagrant ssh -c "cp /etc/kubeconfig.yml ~/.kube/config"
vagrant ssh -c "cat ~/.kube/config" > ~/.kube/config
sed -i '/Connection to 127.0.0.1 closed/d' ~/.kube/config

# Setting up host to talk to insecure registry on VM for linux host.
if [[ $OSTYPE = "linux"* ]]; then
  echo "Adding insecure registry to docker daemon in host vm..."
  echo "You old docker daemon file can be found at /lib/systemd/system/docker.service_old"
  sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
  sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service
  sudo systemctl daemon-reload
  sudo systemctl restart docker
fi
