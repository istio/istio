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
#
# Initialization script responsible for setting up port forwarding for Istio sidecar.

function usage() {
  echo "${0}"
}

function dump {
    iptables-save
    ip6tables-save
}

trap dump EXIT

for cmd in iptables ip6tables
do
  # Remove the old chains, to generate new configs.
  ${cmd} -t nat -D PREROUTING -p tcp -j ISTIO_INBOUND 2>/dev/null
  ${cmd} -t mangle -D PREROUTING -p tcp -j ISTIO_INBOUND 2>/dev/null
  ${cmd} -t nat -D OUTPUT -p tcp -j ISTIO_OUTPUT 2>/dev/null

  # Flush and delete the istio chains.
  ${cmd} -t nat -F ISTIO_OUTPUT 2>/dev/null
  ${cmd} -t nat -X ISTIO_OUTPUT 2>/dev/null
  ${cmd} -t nat -F ISTIO_INBOUND 2>/dev/null
  ${cmd} -t nat -X ISTIO_INBOUND 2>/dev/null
  ${cmd} -t mangle -F ISTIO_INBOUND 2>/dev/null
  ${cmd} -t mangle -X ISTIO_INBOUND 2>/dev/null
  ${cmd} -t mangle -F ISTIO_DIVERT 2>/dev/null
  ${cmd} -t mangle -X ISTIO_DIVERT 2>/dev/null
  ${cmd} -t mangle -F ISTIO_TPROXY 2>/dev/null
  ${cmd} -t mangle -X ISTIO_TPROXY 2>/dev/null

  # Must be last, the others refer to it
  ${cmd} -t nat -F ISTIO_REDIRECT 2>/dev/null
  ${cmd} -t nat -X ISTIO_REDIRECT 2>/dev/null
  ${cmd} -t nat -F ISTIO_IN_REDIRECT 2>/dev/null
  ${cmd} -t nat -X ISTIO_IN_REDIRECT 2>/dev/null
done