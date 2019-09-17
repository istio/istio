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

SCRIPTPATH="$(cd "$(dirname "$0")" || exit; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"
# shellcheck source=tests/e2e/local/common_linux.sh
source "${ROOTDIR}/common_linux.sh"

check_apt_get

install_curl

install_docker

# install crictl
CRICTL_VERSION="v1.11.1"
curl -LO https://github.com/kubernetes-incubator/cri-tools/releases/download/$CRICTL_VERSION/crictl-$CRICTL_VERSION-linux-amd64.tar.gz
sudo tar zxvf crictl-$CRICTL_VERSION-linux-amd64.tar.gz -C /usr/local/bin
rm -f crictl-$CRICTL_VERSION-linux-amd64.tar.gz

# install kubelet kubeadm kubectl via apt-get way
sudo apt-get install -y apt-transport-https
curl -s https://packages.cloud.google.com/apt/doc/apt-key.gpg | sudo apt-key add -
cat <<EOF >/etc/apt/sources.list.d/kubernetes.list
deb http://apt.kubernetes.io/ kubernetes-xenial main
EOF
sudo apt-get --quiet -y update
sudo apt-get install -y kubelet kubeadm kubectl
sudo apt-mark hold kubelet kubeadm kubectl

echo "Everything installed for you and you are ready to go!"
