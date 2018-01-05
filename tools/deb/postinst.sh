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
set -e

action="$1"
oldversion="$2"

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

chown istio-proxy.istio-proxy /var/lib/istio/envoy /var/lib/istio/config /var/log/istio /var/lib/istio/config/mesh /var/lib/istio/proxy

