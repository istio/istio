#!/bin/bash
#
# Copyright 2017 Istio Authors. All Rights Reserved.
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
#
################################################################################

# Script to install istio components for the raw VM.

# Environment variable pointing to the generated Istio configs and binaries.
# TODO: use curl or tar to fetch the artifacts.
ISTIO_STAGING=${ISTIO_STAGING:-.}

# Configure network for istio use, using DNSMasq.
# Will use the generated "kubedns" file.
function istioNetworkInit() {
  if [[ ! -r /usr/sbin/dnsmasq ]] ; then
    apt-get update
    sudo apt-get -y install dnsmasq
  fi

  # Copy config files for DNS
  chmod go+r ${ISTIO_STAGING}/kubedns
  cp ${ISTIO_STAGING}/kubedns /etc/dnsmasq.d
  systemctl restart dnsmasq

  # Update DHCP - if needed
  grep "^prepend domain-name-servers 127.0.0.1;" /etc/dhcp/dhclient.conf > /dev/null
  if [[ $? != 0 ]]; then
    echo 'prepend domain-name-servers 127.0.0.1;' >> /etc/dhcp/dhclient.conf
    # TODO: find a better way to re-trigger dhclient
    dhclient -v -1
  fi
}

# Install istio components and certificates. The admin (directly or using tools like ansible)
# will generate and copy the files and install the packages on each machine.
function istioInstall() {
  mkdir -p /etc/certs

  cp ${ISTIO_STAGING}/*.pem /etc/certs

  # Cluster settings - the CIDR in particular.
  cp ${ISTIO_STAGING}/cluster.env /var/lib/istio/envoy

  chown -R istio-proxy /etc/certs
  chown -R istio-proxy /var/lib/istio/envoy

  ISTIO_VERSION=${ISTIO_VERSION:-0.2.4}

  # Current URL for the debian files artifacts. Will be replaced by a proper apt repo.
  DEBURL=http://gcsweb.istio.io/gcs/istio-release/releases/${ISTIO_VERSION}/deb
  curl -L ${DEBURL}/istio-agent-release.deb > istio-agent-release.deb
  curl -L ${DEBURL}/istio-auth-node-agent-release.deb > istio-auth-node-agent-release.deb
  curl -L ${DEBURL}/istio-proxy-release.deb > istio-proxy-release.deb

  # Install istio binaries
  dpkg -i ${ISTIO_STAGING}/istio-proxy-envoy.deb
  dpkg -i ${ISTIO_STAGING}/istio-agent.deb
  dpkg -i ${ISTIO_STAGING}/istio-auth-node-agent.deb
}

function istioRestart() {
    # Start or restart istio
    systemctl status istio > /dev/null
    if [[ $? = 0 ]]; then
      systemctl restart istio
    else
      systemctl start istio
    fi
}

if [[ ${1:-} == "initNetwork" ]] ; then
  istioNetworkInit
elif [[ ${1:-} == "istioInstall" ]] ; then
  istioInstall
  istioRestart
elif [[ ${1:-} == "help" ]] ; then
  echo "$0 initNetwork: Configure DNS"
  echo "$0 istioInstall: Install istio components"
else
  istioNetworkInit
  istioInstall
  istioRestart
fi
