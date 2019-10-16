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

# Cleanup linux host setup to talk to kubernetes cluster on Vagrant VM.
cp ~/.kube/config_old ~/.kube/config
rm -rf ~/.kube/config_old

# Cleanup Setup on host to talk to insecure registry on VM.
sudo cp /lib/systemd/system/docker.service_old /lib/systemd/system/docker.service
sudo rm -rf /lib/systemd/system/docker.service_old
sudo systemctl daemon-reload
sudo systemctl restart docker
