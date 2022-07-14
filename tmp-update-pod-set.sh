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

# shellcheck disable=all

set -x

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

. $WD/config.sh

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

if [ -z "${IPSET}" ]; then
  IPSET=ipset
  if [ "${K8S_TYPE}" == gke ]; then
    IPSET="toolbox ipset"
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
      docker exec -it "$node_name" $cmd
    fi
  elif [ "${K8S_TYPE}" == aws ]; then
    NODE_IP=$(kubectl get nodes -l kubernetes.io/hostname="$node_name" -o jsonpath="{.items[*].status.addresses[?(@.type=='ExternalIP')].address}")
    # aws doesn't let you ssh as root, and you need root to use iptables
    # thus we ssh as ec2-user and then prefix all our commands with sudo su.
    # if unset, read from stdin
    if [ -z "${cmd}" ]; then
      ssh -i "$SSH_KEY" ec2-user@"$NODE_IP" "sudo su"
    else
      # shellcheck disable=SC2086
      ssh -t -i "$SSH_KEY" ec2-user@"$NODE_IP" "sudo $cmd"
    fi
  elif [ "${K8S_TYPE}" == gke ]; then
    if [ -z "${cmd}" ]; then
      gcloud compute ssh "$node_name" --command="sudo su"
    else
      # shellcheck disable=SC2086
      gcloud compute ssh "$node_name" --command="sudo bash $cmd"
    fi
  else
    echo "not a supported k8s deployment type"
    exit 1
  fi
}

function add_ip_set() {
    local node_name="$1"
    PODS="$(kubectl get pods --all-namespaces --field-selector spec.nodeName="$node_name" -lambient-type=workload -o custom-columns=:.status.podIP --no-headers)"
    exec_on_node "$node_name" <<EOF
    $IPSET create tmp-uproxy-pods-ips hash:ip
    $IPSET flush tmp-uproxy-pods-ips
EOF
        exec_on_node "$node_name" <<EOF

# try host side veth
HOST_IP="\$(ip addr show | grep 'inet.*veth' | head -n1 | awk '{print \$2}' | cut -d/ -f1)"
if [ -z "\$HOST_IP" ]; then
  # try eth0
  HOST_IP=\$(ip addr show eth0 | grep "inet\b" | awk '{print \$2}' | cut -d/ -f1)
fi

PODS="$PODS"

# flush secondary table
ip route flush table $INBOUND_ROUTE_TABLE2
# fill in secondary table
for pip in \$PODS; do
        $IPSET add tmp-uproxy-pods-ips \$pip
        ip route add table $INBOUND_ROUTE_TABLE2 \$pip/32 via $UPROXY_INBOUND_TUN_IP dev $INBOUND_TUN src \$HOST_IP
done

# activate secondary table
ip rule add priority $(($IP_RULE_BASE+3)) goto $(($IP_RULE_BASE+5))

# add everything to the primary table
ip route flush table $INBOUND_ROUTE_TABLE
for pip in \$PODS; do
        ip route add table $INBOUND_ROUTE_TABLE \$pip/32 via $UPROXY_INBOUND_TUN_IP dev $INBOUND_TUN src \$HOST_IP
done
# deactivate the secondary table
ip rule del priority $(($IP_RULE_BASE+3)) goto $(($IP_RULE_BASE+5))

EOF

    exec_on_node "$node_name" <<EOF
    $IPSET swap tmp-uproxy-pods-ips uproxy-pods-ips
    $IPSET destroy tmp-uproxy-pods-ips
EOF
}

while true; do
    for nodeName in $(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o custom-columns=:.metadata.name --no-headers); do
        add_ip_set "$nodeName"
    done
    if [ -n "${UPDATE_IPSET_ONCE}" ]; then
      break
    fi
    sleep 1
done