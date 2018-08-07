#!/bin/bash

# Setting up docker daemon on host
echo "Adding insecure registry to docker daemon in host..."
echo "You old docker daemon file can be found at /lib/systemd/system/docker.service_old"
if ! sudo ls /lib/systemd/system/docker.service_old; then
    sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
else
    echo "There is an old docker.service_old file on your system."
    read -p "If you believe it's outdated, we can update it[default: no]: " update
    overrwriteExisting=${update:-"no"}
    if [[ $overrwriteExisting = *"y"* ]] || [[ $overrwriteExisting = *"Y"* ]]; then
        sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
    fi
fi
echo "sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service"
sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service
sudo systemctl daemon-reload
sudo systemctl restart docker

# Set route rule to access VM
echo "Set route rule to access VM"
sudo ip route add 10.0.0.0/24 via 10.10.0.2

echo "$(tput setaf 1)Please run docker info and make sure insecure registry address is updated to 10.10.0.2:5000$(tput sgr 0)"
echo "Docker daemon setup done."