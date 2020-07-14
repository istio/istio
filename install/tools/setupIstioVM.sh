#!/bin/bash
#
# Copyright Istio Authors. All Rights Reserved.
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

function istioVersionSource() {
  echo "Sourced ${ISTIO_STAGING}/istio.VERSION"
  cat "${ISTIO_STAGING}/istio.VERSION"
  # shellcheck disable=SC1090
  source "${ISTIO_STAGING}/istio.VERSION"
}

# Configure network for istio use, using DNSMasq.
# Will use the generated "kubedns" file.
function istioNetworkInit() {
  if [[ ! -r /etc/dnsmasq.d ]] ; then
    echo "*** Running apt-get update..."
    apt-get update > /dev/null
    echo "*** Running apt-get install dnsmasq..."
    apt-get --no-install-recommends -y install dnsmasq
  fi

  # Copy config files for DNS
  chmod go+r "${ISTIO_STAGING}/kubedns"
  cp "${ISTIO_STAGING}/kubedns" /etc/dnsmasq.d
  systemctl restart dnsmasq

  # Update DHCP - if needed
  if ! grep "^prepend domain-name-servers 127.0.0.1;" /etc/dhcp/dhclient.conf > /dev/null; then
    echo 'prepend domain-name-servers 127.0.0.1;' >> /etc/dhcp/dhclient.conf
    # TODO: find a better way to re-trigger dhclient
    dhclient -v -1
  fi
}

# Install istio components and certificates. The admin (directly or using tools like ansible)
# will generate and copy the files and install the packages on each machine.
function istioInstall() {
  echo "*** Fetching istio packages..."
  # Current URL for the debian files artifacts. Will be replaced by a proper apt repo.
  rm -f istio-sidecar.deb
  echo "curl -f -L ${PILOT_DEBIAN_URL}/istio-sidecar.deb > ${ISTIO_STAGING}/istio-sidecar.deb"
  curl -f -L "${PILOT_DEBIAN_URL}/istio-sidecar.deb" > "${ISTIO_STAGING}/istio-sidecar.deb"

  # Install istio binaries
  dpkg -i "${ISTIO_STAGING}/istio-sidecar.deb"

  mkdir -p /etc/certs

  # shellcheck disable=SC2086
  cp ${ISTIO_STAGING}/*.pem /etc/certs

  # Cluster settings - the CIDR in particular.
  cp "${ISTIO_STAGING}/cluster.env" /var/lib/istio/envoy

  chown -R istio-proxy /etc/certs
  chown -R istio-proxy /var/lib/istio/envoy

  # Useful to test VM extension to istio
  apt-get --no-install-recommends -y install host
}

function istioRestart() {
    echo "*** Restarting istio proxy..."
    # Start or restart istio envoy
    if systemctl status istio > /dev/null; then
      systemctl restart istio
    else
      systemctl start istio
    fi
}

if [[ ${1:-} == "initNetwork" ]] ; then
  istioNetworkInit
elif [[ ${1:-} == "istioInstall" ]] ; then
  istioVersionSource
  istioInstall
  istioRestart
elif [[ ${1:-} == "help" ]] ; then
  echo "$0 initNetwork: Configure DNS"
  echo "$0 istioInstall: Install istio components"
else
  istioVersionSource
  istioNetworkInit
  istioInstall
  istioRestart
fi
