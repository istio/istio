#!/usr/bin/env bash

# Copyright 2020 Istio Authors
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

WORKER_NODES="$(kubectl get nodes -l '!node-role.kubernetes.io/control-plane' -o custom-columns=:.metadata.name --no-headers)"

# if k8s type var not set, default to kind
if [ -z "${K8S_TYPE}" ]; then
   K8S_TYPE='kind'
fi

# update interface vars based on type
if [ "${K8S_TYPE}" == aws ]; then
  INTERFACE_PREFIX=eni
  # if k8s type var not set, default to kind
  if [ -z "${SSH_KEY}" ]; then
     echo "need to set ssh key location for updating cluster nodes"
     exit 1
  fi
elif [ "${K8S_TYPE}" == calico ]; then
  INTERFACE_PREFIX=cali
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

# if iptables var not set, default to iptables-nft, but detect legacy.
if [ -z "${IPTABLES}" ]; then
  IPTABLES=iptables-nft
  for node in ${WORKER_NODES}; do
    if ! exec_on_node "$node" 'iptables-nft --help'; then
      IPTABLES=iptables
      break
    elif exec_on_node "$node" 'iptables-nft-save' | grep 'Warning: iptables-legacy'; then
      IPTABLES=iptables-legacy
      break
    fi
  done
fi

if [ -z "${IPSET}" ]; then
  IPSET=ipset
  if [ "${K8S_TYPE}" == gke ]; then
    IPSET="toolbox ipset"
  fi
fi

if [[ "${2:-}" == clean ]]; then
  # clean up previous chains
  for node in ${WORKER_NODES}; do
    exec_on_node "$node" <<EOF
    $IPTABLES -t nat -F uproxy-PREROUTING
    $IPTABLES -t nat -F uproxy-POSTROUTING
    $IPTABLES -t mangle -F uproxy-PREROUTING
    $IPTABLES -t mangle -F uproxy-FORWARD
    $IPTABLES -t mangle -F uproxy-INPUT
    $IPTABLES -t mangle -F uproxy-OUTPUT
    # we flush because sometimes -X doesn't work.
    $IPTABLES -t nat -D PREROUTING -j uproxy-PREROUTING
    $IPTABLES -t nat -X uproxy-PREROUTING
    $IPTABLES -t nat -D POSTROUTING -j uproxy-POSTROUTING
    $IPTABLES -t nat -X uproxy-POSTROUTING
    $IPTABLES -t mangle -D PREROUTING -j uproxy-PREROUTING
    $IPTABLES -t mangle -X uproxy-PREROUTING
    $IPTABLES -t mangle -D FORWARD -j uproxy-FORWARD
    $IPTABLES -t mangle -X uproxy-FORWARD
    $IPTABLES -t mangle -D INPUT -j uproxy-INPUT
    $IPTABLES -t mangle -X uproxy-INPUT
    $IPTABLES -t mangle -D OUTPUT -j uproxy-OUTPUT
    $IPTABLES -t mangle -X uproxy-OUTPUT

    ip route flush table $INBOUND_ROUTE_TABLE
    ip route flush table $OUTBOUND_ROUTE_TABLE
    ip route flush table $PROXY_ROUTE_TABLE
    
    ip rule del priority 20000
    ip rule del priority 20001
    ip rule del priority 20002
    ip rule del priority 20003
    ip rule del priority 20004
    ip rule del priority 20005
    ip rule del priority 20006
    ip rule del priority 20007

    ip link del name $INBOUND_TUN
    ip link del name $OUTBOUND_TUN

    $IPSET destroy uproxy-pods-ips

    echo If you need to clean the tunnel interface in the uproxy pod consider restarting it.
EOF
  done

  exit 0
fi


for node in ${WORKER_NODES}; do
exec_on_node "$node" <<EOF
  if ! $IPSET --help; then
    if apt --help; then
      apt update
      apt install ipset -y
    elif yum --help; then
      yum makecache --refresh
      yum -y install ipset
    else
      echo "*** ERROR no supported way to install ipset ***"
    fi
  fi
EOF
done

# add our tables if not exist yet, flush them if they do exist.
for node in ${WORKER_NODES}; do
  cat "$(dirname -- $0)/config.sh" <(echo IPTABLES=$IPTABLES) <(echo INTERFACE_PREFIX=$INTERFACE_PREFIX) <(echo K8S_TYPE=$K8S_TYPE) $WD/redirect-worker.sh | exec_on_node "$node"
done


if [ "${DEBUG}" == "1" ]; then
  # create a route table for the inbound traffic
for node in ${WORKER_NODES}; do
  exec_on_node "$node" <<EOF
$IPTABLES-save | grep -v LOG | $IPTABLES-restore
$IPTABLES -t mangle -I PREROUTING -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "mangle pre [$node] "
$IPTABLES -t mangle -I POSTROUTING -o ${INTERFACE_PREFIX}+ -j LOG --log-prefix "mangle post [$node] "
$IPTABLES -t mangle -I INPUT -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "mangle inp [$node] "
$IPTABLES -t mangle -I OUTPUT -o ${INTERFACE_PREFIX}+ -j LOG --log-prefix "mangle out [$node] "
$IPTABLES -t mangle -I FORWARD -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "mangle fw [$node] "
$IPTABLES -t nat -I POSTROUTING -o ${INTERFACE_PREFIX}+ -j LOG --log-prefix "nat post [$node] "
$IPTABLES -t nat -I INPUT -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "nat inp [$node] "
$IPTABLES -t nat -I OUTPUT -o ${INTERFACE_PREFIX}+ -j LOG --log-prefix "nat out [$node] "
$IPTABLES -t nat -I PREROUTING -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "nat pre [$node] "
$IPTABLES -t raw -I PREROUTING -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "raw pre [$node] "
$IPTABLES -t raw -I OUTPUT -o ${INTERFACE_PREFIX}+ -j LOG --log-prefix "raw out [$node] "
$IPTABLES -t filter -I FORWARD -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "filt fw [$node] "
$IPTABLES -t filter -I OUTPUT -o ${INTERFACE_PREFIX}+ -j LOG --log-prefix "filt out [$node] "
$IPTABLES -t filter -I INPUT -i ${INTERFACE_PREFIX}+ -j LOG --log-prefix "filt inp [$node] "


$IPTABLES -t mangle -I PREROUTING -i istio+ -j LOG --log-prefix "mangle pre [$node|tun] "
$IPTABLES -t mangle -I POSTROUTING -o istio+ -j LOG --log-prefix "mangle post [$node|tun] "
$IPTABLES -t mangle -I INPUT -i istio+ -j LOG --log-prefix "mangle inp [$node|tun] "
$IPTABLES -t mangle -I OUTPUT -o istio+ -j LOG --log-prefix "mangle out [$node|tun] "
$IPTABLES -t mangle -I FORWARD -i istio+ -j LOG --log-prefix "mangle fw [$node|tun] "
$IPTABLES -t nat -I POSTROUTING -o istio+ -j LOG --log-prefix "nat post [$node|tun] "
$IPTABLES -t nat -I INPUT -i istio+ -j LOG --log-prefix "nat inp [$node|tun] "
$IPTABLES -t nat -I OUTPUT -o istio+ -j LOG --log-prefix "nat out [$node|tun] "
$IPTABLES -t nat -I PREROUTING -i istio+ -j LOG --log-prefix "nat pre [$node|tun] "
$IPTABLES -t raw -I PREROUTING -i istio+ -j LOG --log-prefix "raw pre [$node|tun] "
$IPTABLES -t raw -I OUTPUT -o istio+ -j LOG --log-prefix "raw out [$node|tun] "
$IPTABLES -t filter -I FORWARD -i istio+ -j LOG --log-prefix "filt fw [$node|tun] "
$IPTABLES -t filter -I OUTPUT -o istio+ -j LOG --log-prefix "filt out [$node|tun] "
$IPTABLES -t filter -I INPUT -i istio+ -j LOG --log-prefix "filt inp [$node|tun] "

# log martian packets
echo 1 > /proc/sys/net/ipv4/conf/all/log_martians

EOF

done

for uproxypod in $(kubectl get pods -n istio-system -lapp=uproxy -o custom-columns=:.metadata.name --no-headers); do
  unset DEBUG
  kubectl exec -i -n istio-system "$uproxypod" -- /bin/sh -x <<EOF
$IPTABLES-save | grep -v LOG | $IPTABLES-restore
$IPTABLES -t mangle -I PREROUTING -j LOG --log-prefix "mangle pre [$uproxypod] "
$IPTABLES -t mangle -I POSTROUTING -j LOG --log-prefix "mangle post [$uproxypod] "
$IPTABLES -t mangle -I INPUT -j LOG --log-prefix "mangle inp [$uproxypod] "
$IPTABLES -t mangle -I OUTPUT -j LOG --log-prefix "mangle out [$uproxypod] "
$IPTABLES -t mangle -I FORWARD -j LOG --log-prefix "mangle fw [$uproxypod] "
$IPTABLES -t nat -I POSTROUTING -j LOG --log-prefix "nat post [$uproxypod] "
$IPTABLES -t nat -I INPUT -j LOG --log-prefix "nat inp [$uproxypod] "
$IPTABLES -t nat -I OUTPUT -j LOG --log-prefix "nat out [$uproxypod] "
$IPTABLES -t nat -I PREROUTING -j LOG --log-prefix "nat pre [$uproxypod] "
$IPTABLES -t raw -I PREROUTING -j LOG --log-prefix "raw pre [$uproxypod] "
$IPTABLES -t raw -I OUTPUT -j LOG --log-prefix "raw out [$uproxypod] "
$IPTABLES -t filter -I FORWARD -j LOG --log-prefix "filt fw [$uproxypod] "
$IPTABLES -t filter -I OUTPUT -j LOG --log-prefix "filt out [$uproxypod] "
$IPTABLES -t filter -I INPUT -j LOG --log-prefix "filt inp [$uproxypod] "

EOF

done

fi