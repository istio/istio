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

# if k8s type var not set, default to kind
if [ -z "${K8S_TYPE}" ]; then
   K8S_TYPE='kind'
fi

if [ "${K8S_TYPE}" == aws ]; then
  # if k8s type var not set, default to kind
  if [ -z "${SSH_KEY}" ]; then
     echo "need to set ssh key location for updating cluster nodes"
     exit 1
  fi
fi

function exec_on_node() {
  local node_name="$1"
  local cmd="$2"
  if [ "${K8S_TYPE}" == kind ]; then
    # if unset, read from stdin
    if [ -z "${cmd}" ]; then
      docker exec -i "$node_name" sh -x
    else
      # shellcheck disable=SC2086
      docker exec -i "$node_name" $cmd
    fi
  elif [ "${K8S_TYPE}" == aws ]; then
    NODE_IP=$(kubectl get nodes -l kubernetes.io/hostname="$node_name" -o jsonpath="{.items[*].status.addresses[?(@.type=='ExternalIP')].address}")
    # rewrite with sudo to get root shell in front
    ROOT_CMD="sudo su $cmd"
    # aws doesn't let you ssh as root, and you need root to use iptables
    # thus we ssh as ec2-user and then prefix all our commands with sudo su
    ssh -i "$SSH_KEY" ec2-user@"$NODE_IP" "$ROOT_CMD"
  else
    echo "not a supported k8s deployment type"
    exit 1
  fi
}

function add_ip_set() {
    local node_name="$1"
    PODS="$(kubectl get pods --all-namespaces --field-selector spec.nodeName="$node_name" -lambient-type=workload -o custom-columns=:.status.podIP --no-headers)"
    exec_on_node "$node_name" <<EOF
    ipset create tmp-uproxy-pods-ips hash:ip
    ipset flush tmp-uproxy-pods-ips
EOF

    for pip in $PODS; do
        exec_on_node "$node_name" <<EOF
        ipset add tmp-uproxy-pods-ips $pip
EOF
    done

    exec_on_node "$node_name" <<EOF
    ipset swap tmp-uproxy-pods-ips uproxy-pods-ips
    ipset destroy tmp-uproxy-pods-ips
EOF
}

while true; do
    for nodeName in $(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o custom-columns=:.metadata.name --no-headers); do
        add_ip_set "$nodeName"
    done
    sleep 1
done
