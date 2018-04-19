#!/usr/bin/env bash

mkdir -p $ISTIO
# We cannot directly set up synced folder between $ISTIO in host machine and $ISTIO in VM.
# Because at VM boot up stage synced folder setup comes before privision bootstrap.sh. 
# Therefore directory $ISTIO in VM does not exist when Vagrant sets up synced folder.
# We synced $ISTIO from host to /istio.io in VM, and create a softlink between /istio.io/istio and $ISTIO/istio.
sudo ln -s /istio.io/istio/ $ISTIO/istio

# Copy kube config
echo "Copy kube config file to ~/.kube/config"
mkdir $HOME/.kube
cp /etc/kubeconfig.yml $HOME/.kube/config

# Deploy kubernetes local docker registry."
echo "Deploy kubernetes local docker registry."
kubectl apply -f $ISTIO/istio/tests/util/localregistry/localregistry.yaml

# install debugger Delve
go get github.com/derekparker/delve/cmd/dlv
