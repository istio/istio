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

# Setting up kubectl on host to talk to kubernetes cluster on Vagrant VM.

# Save and unset KUBECONFIG in case users set it pointing to a k8s cluster
export KUBECONFIG_SAVED=$KUBECONFIG
unset KUBECONFIG

# Set kube config file on host
if ! ls ~/.kube/config_old > /dev/null; then
    if ls ~/.kube/config > /dev/null; then
    	cp ~/.kube/config ~/.kube/config_old
    	echo "your old ~/.kube/config file can be found at ~/.kube/config_old"
    fi
else
    echo "There is an old ~/.kube/config_old file on your system."
    read -p "If you believe it's outdated, we can update it[default: no]: " -r update
    overwriteExisting=${update:-"no"}
    if [[ $overwriteExisting = *"y"* ]] || [[ $overwriteExisting = *"Y"* ]]; then
        cp ~/.kube/config ~/.kube/config_old
    fi
fi
vagrant ssh -c "cat ~/.kube/config" > ~/.kube/config

echo "kubectl setup done."
