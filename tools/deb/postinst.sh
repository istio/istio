#!/bin/bash
#
# Copyright 2017, 2018 Istio Authors. All Rights Reserved.
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
set -e

umask 022

if ! getent passwd istio-proxy >/dev/null; then
    addgroup --system istio-proxy
    adduser --system --group --home /var/lib/istio istio-proxy
fi

if [ ! -e /etc/istio ]; then
   # Backward compat.
   ln -s /var/lib/istio /etc/istio
fi

mkdir -p /var/lib/istio/envoy
mkdir -p /var/lib/istio/proxy
mkdir -p /var/lib/istio/config
mkdir -p /var/log/istio

touch /var/lib/istio/config/mesh

mkdir -p /etc/certs
chown istio-proxy.istio-proxy /etc/certs

chown istio-proxy.istio-proxy /var/lib/istio/envoy /var/lib/istio/config /var/log/istio /var/lib/istio/config/mesh /var/lib/istio/proxy
chmod o+rx /usr/local/bin/{envoy,pilot-agent,node_agent}

# pilot-agent and envoy may run with effective uid 0 in order to run envoy with
# CAP_NET_ADMIN, so any iptables rule matching on "-m owner --uid-owner
# istio-proxy" will not match connections from those processes anymore.
# Instead, rely on the process's effective gid being istio-proxy and create a
# "-m owner --gid-owner istio-proxy" iptables rule in istio-iptables.sh.
chmod 2755 /usr/local/bin/{envoy,pilot-agent}
