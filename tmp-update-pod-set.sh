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

function add_ip_set() {
    local node_name="$1"
    PODS="$(kubectl get pods --field-selector spec.nodeName==$node_name -lambient-type=workload -o custom-columns=:.status.podIP --no-headers)"
    docker exec -i "$node_name" sh <<EOF
    ipset create tmp-uproxy-pods-ips hash:ip
    ipset flush tmp-uproxy-pods-ips
EOF

    for pip in $PODS; do
        docker exec -i "$node_name" ipset add tmp-uproxy-pods-ips $pip
    done

    docker exec -i "$node_name" sh <<EOF
    ipset swap tmp-uproxy-pods-ips uproxy-pods-ips
    ipset destroy tmp-uproxy-pods-ips
EOF

}


while true; do
    for nodeName in $(kubectl get nodes -o custom-columns=:.metadata.name --no-headers | grep worker); do
        add_ip_set $nodeName
    done
    sleep 1
done