#!/usr/bin/env bash

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

set -x

INGRESS_HOST=$(kubectl -n istio-system get pod -l istio=ingressgateway -o jsonpath='{.items[0].status.hostIP}')
export INGRESS_HOST

SECURE_INGRESS_PORT=$(kubectl -n istio-system get service istio-ingressgateway -o jsonpath='{.spec.ports[?(@.port==443)].nodePort}')
export SECURE_INGRESS_PORT

curl -v -HHost:httpbin.example.com \
--resolve httpbin.example.com:"$SECURE_INGRESS_PORT":"$INGRESS_HOST" \
--cacert httpbin.example.com/2_intermediate/certs/ca-chain.cert.pem \
https://httpbin.example.com:"$SECURE_INGRESS_PORT"/status/418