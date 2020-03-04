#!/bin/bash -xe

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

# Cleanup all namespaces

for namespace in istio-system istio-control istio-control-master istio-ingress istio-telemetry istio-cni; do
    kubectl delete namespace $namespace --wait --ignore-not-found
done

ACTIVE_NAMESPACES=$(kubectl get namespaces --no-headers -l istio-env -o=custom-columns=NAME:.metadata.name)
for ns in $ACTIVE_NAMESPACES; do
    kubectl label namespaces "${ns}" istio-env-
done