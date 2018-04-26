#!/bin/bash

# Setting up docker daemon on host
echo "Adding insecure registry to docker daemon in host..."
echo "You old docker daemon file can be found at /lib/systemd/system/docker.service_old"
sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
echo "sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:${IstioDport}/' /lib/systemd/system/docker.service"
sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:'"$IstioDport"'/' /lib/systemd/system/docker.service
sudo systemctl daemon-reload
sudo systemctl restart docker

# Set route rule to access VM
echo "Set route rule to access VM"
sudo ip route add 10.0.0.0/24 via 10.10.0.2

echo "$(tput setaf 1)Please run docker info and make sure insecure registry address is updated to 10.10.0.2:${IstioDport}$(tput sgr 0)"
