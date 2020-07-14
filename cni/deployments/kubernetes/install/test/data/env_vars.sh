#!/bin/bash

# Copyright 2019 Istio Authors
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

# shellcheck disable=SC2034
KUBE_DNS_SERVICE_PORT=53
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_TCP_PROTO=tcp
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_UDP=udp://10.110.0.10:53
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_UDP_PROTO=udp
# shellcheck disable=SC2034
KUBERNETES_PORT_443_TCP_PROTO=tcp
# shellcheck disable=SC2034
KUBERNETES_PORT_443_TCP_ADDR=10.110.0.1
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_UDP_ADDR=10.110.0.10
# shellcheck disable=SC2034
KUBERNETES_PORT=tcp://10.110.0.1:443
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_TCP_ADDR=10.110.0.10
# shellcheck disable=SC2034
KUBE_DNS_PORT=udp://10.110.0.10:53
# shellcheck disable=SC2034
KUBERNETES_SERVICE_PORT_HTTPS=443
# shellcheck disable=SC2034
KUBERNETES_PORT_443_TCP_PORT=443
# shellcheck disable=SC2034
KUBERNETES_PORT_443_TCP=tcp://10.110.0.1:443
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_TCP_PORT=53
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_TCP=tcp://10.110.0.10:53
# shellcheck disable=SC2034
KUBERNETES_SERVICE_PORT=443
# shellcheck disable=SC2034
KUBE_DNS_SERVICE_PORT_DNS=53
# shellcheck disable=SC2034
KUBE_DNS_SERVICE_PORT_DNS_TCP=53
# shellcheck disable=SC2034
KUBERNETES_SERVICE_HOST=10.110.0.1
# shellcheck disable=SC2034
KUBE_DNS_PORT_53_UDP_PORT=53
# shellcheck disable=SC2034
KUBE_DNS_SERVICE_HOST=10.110.0.10
