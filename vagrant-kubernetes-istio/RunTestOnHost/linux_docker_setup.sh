#!/bin/bash

# Setting up host to talk to insecure registry on VM.
echo "Adding insecure registry to docker daemon in host vm..."
echo "You old docker daemon file can be found at /lib/systemd/system/docker.service_old"
sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service
sudo systemctl daemon-reload
sudo systemctl restart docker
