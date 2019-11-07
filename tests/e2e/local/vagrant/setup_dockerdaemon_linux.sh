#!/bin/bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Setting up docker daemon on host
echo "Adding insecure registry to docker daemon in host..."
echo "You old docker daemon file can be found at /lib/systemd/system/docker.service_old"
if ! sudo ls /lib/systemd/system/docker.service_old; then
    sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
else
    echo "There is an old docker.service_old file on your system."
    read -p "If you believe it's outdated, we can update it[default: no]: " -r update
    overwriteExisting=${update:-"no"}
    if [[ $overwriteExisting = *"y"* ]] || [[ $overwriteExisting = *"Y"* ]]; then
        sudo cp /lib/systemd/system/docker.service /lib/systemd/system/docker.service_old
    fi
fi
echo "sudo sed -i 's/ExecStart=\\/usr\\/bin\\/dockerd -H fd:\\/\\//ExecStart=\\/usr\\/bin\\/dockerd -H fd:\\/\\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service"
sudo sed -i 's/ExecStart=\/usr\/bin\/dockerd -H fd:\/\//ExecStart=\/usr\/bin\/dockerd -H fd:\/\/ --insecure-registry 10.10.0.2:5000/' /lib/systemd/system/docker.service
sudo systemctl daemon-reload
sudo systemctl restart docker

# Set route rule to access VM
echo "Set route rule to access VM"
sudo ip route add 10.0.0.0/24 via 10.10.0.2

echo "$(tput setaf 1)Please run docker info and make sure insecure registry address is updated to 10.10.0.2:5000$(tput sgr 0)"
echo "Docker daemon setup done."
