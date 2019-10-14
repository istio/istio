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

for it in {1..15}
do
  stats=$(kubectl -n istio-system exec "$(kubectl get pod -l istio=ingressgateway -n istio-system \
  -o jsonpath={.items..metadata.name})" -c istio-proxy -- \
  curl 127.0.0.1:15000/stats?filter=listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds)
  if [[ "$stats" == "listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds: 1" ]]; then
    echo "Envoy stats listener.0.0.0.0_443.server_ssl_socket_factory.ssl_context_update_by_sds meets expectation with ${it} retries"
    exit
  else
    sleep 2s
  fi
done

echo "ingress gateway SDS stats does not meet in 30 seconds. Expected 1 but got ${stats}" >&2