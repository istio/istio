#!/bin/bash

# Cleanup linux host setup to talk to kubernetest cluster on Vagrant VM.
cp ~/.kube/config_old ~/.kube/config
rm -rf ~/.kube/config_old

# Cleanup Setup on host to talk to insecure registry on VM.
sudo cp /lib/systemd/system/docker.service_old /lib/systemd/system/docker.service
sudo rm -rf /lib/systemd/system/docker.service_old
sudo systemctl daemon-reload
sudo systemctl restart docker
