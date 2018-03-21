#!/bin/bash
# Portions Copyright 2016 The Kubernetes Authors All rights reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

mount --make-shared /

export CNI_BRIDGE_NETWORK_OFFSET="0.0.1.0"
/dindnet &> /var/log/dind.log 2>&1 < /dev/null &

dockerd \
  --host=unix:///var/run/docker.sock \
  --host=tcp://0.0.0.0:2375 \
  &> /var/log/docker.log 2>&1 < /dev/null &

/minikube start --vm-driver=none \
  --kubernetes-version=v1.9.0
 &> /var/log/minikube-start.log 2>&1 < /dev/null

kubectl config view --merge=true --flatten=true > /kubeconfig
