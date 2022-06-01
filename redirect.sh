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

set -x

# Allocate marks and route tables:
# the main requirement here is that these won't class with other things running on the host.
# note that the outmark needs to be configured by pilot as well, so envoy sets it on outgoing traffic.
# though we can potentially avoid using it and user UID exclusions instead.
# TPROXY port in envoy
POD_OUTBOUND=15001
POD_INBOUND=15008
POD_INBOUND_PLAINTEXT=15006

# TODO: assuming we need only 3 marks, we can use 2 bits of the mark space, instead of 12 currently.
# we should also make the location of these 2 bits configurable, so we can change it based on what is already
# in use.

# socket mark setup
MARK_MASK="0xfff"
MARK="0x4d1/$MARK_MASK"
OUTMARK_MASK="0xfff"
# note that outmark needs to be set in envoy as well, as envoy originates this mark.
OUTMARK="0x4d2/$OUTMARK_MASK"
OUTMARK_RET_MASK="0xfff"
OUTMARK_RET="0x4d3/$OUTMARK_RET_MASK"
# note that outmark needs to be set in envoy as well, as envoy originates this mark.
SKIPMARK="0x4d4/$MARK_MASK"
# prefix for pod network interfaces on the host side
INTERFACE_PREFIX=veth
INCOMING_INTERFACE=eth0
# a route table number number we can use to send traffic to envoy (should be unused).
ROUTE_TABLE=100

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

# if iptables var not set, default to iptables-nft, but detect legacy.
if [ -z "${IPTABLES}" ]; then
  IPTABLES=iptables-nft
  for node in ${WORKER_NODES}; do
    if ! exec_on_node "$node" 'iptables-nft --help'; then
      IPTABLES=iptables
      break
    elif ! exec_on_node "$node" 'iptables-nft-save' | grep 'Warning: iptables-legacy'; then
      IPTABLES=iptables-legacy
      break
    fi
  done
fi

if [[ "${2:-}" == clean ]]; then
  # clean up previous chains
  for node in ${WORKER_NODES}; do
    exec_on_node "$node" <<EOF
    $IPTABLES -t nat -F uproxy-PREROUTING
    $IPTABLES -t mangle -F uproxy-PREROUTING
    $IPTABLES -t mangle -F uproxy-POSTROUTING
    $IPTABLES -t mangle -F uproxy-OUTPUT
    # we flush because sometimes -X doesn't work.
    $IPTABLES -t mangle -D POSTROUTING -j uproxy-POSTROUTING
    $IPTABLES -t mangle -X uproxy-POSTROUTING
    $IPTABLES -t mangle -D OUTPUT -j uproxy-OUTPUT
    $IPTABLES -t mangle -X uproxy-OUTPUT
    $IPTABLES -t mangle -D PREROUTING -j uproxy-PREROUTING
    $IPTABLES -t mangle -X uproxy-PREROUTING
    $IPTABLES -t nat -D PREROUTING -j uproxy-PREROUTING
    $IPTABLES -t nat -X uproxy-PREROUTING
    # Clean up previous rules, if any
    $IPTABLES -t nat -D OUTPUT -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j REDIRECT --to-port $POD_INBOUND
    $IPTABLES -t nat -D OUTPUT -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j LOG --log-prefix="[$node-internal] "
    ip rule del fwmark $MARK lookup $ROUTE_TABLE
    ip rule del fwmark $OUTMARK_RET lookup $ROUTE_TABLE
    ip route del local 0.0.0.0/0 dev lo table $ROUTE_TABLE
    ipset destroy uproxy-pods-ips
EOF
  done

  exit 0
fi


for node in ${WORKER_NODES}; do
exec_on_node "$node" <<EOF
  if ! ipset --help; then
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

exec_on_node "$node" <<EOF
if $IPTABLES -t nat -C PREROUTING -j uproxy-PREROUTING; then
  $IPTABLES -t nat -F uproxy-PREROUTING
  $IPTABLES -t mangle -F uproxy-PREROUTING
  $IPTABLES -t mangle -F uproxy-POSTROUTING
  $IPTABLES -t mangle -F uproxy-OUTPUT
else
  $IPTABLES -t nat -N uproxy-PREROUTING
  $IPTABLES -t nat -I PREROUTING -j uproxy-PREROUTING
  $IPTABLES -t mangle -N uproxy-PREROUTING
  $IPTABLES -t mangle -I PREROUTING -j uproxy-PREROUTING
  $IPTABLES -t mangle -N uproxy-OUTPUT
  $IPTABLES -t mangle -I OUTPUT -j uproxy-OUTPUT
  $IPTABLES -t mangle -N uproxy-POSTROUTING
  $IPTABLES -t mangle -I POSTROUTING -j uproxy-POSTROUTING
fi 
EOF
done

# create our rules
for node in ${WORKER_NODES}; do
exec_on_node "$node" <<EOF
# copy the env vars in
POD_OUTBOUND=$POD_OUTBOUND
POD_INBOUND=$POD_INBOUND
POD_INBOUND_PLAINTEXT=$POD_INBOUND_PLAINTEXT
MARK_MASK=$MARK_MASK
MARK=$MARK
OUTMARK_MASK=$OUTMARK_MASK
OUTMARK=$OUTMARK
SKIPMARK=$SKIPMARK
OUTMARK_RET_MASK=$OUTMARK_RET_MASK
OUTMARK_RET=$OUTMARK_RET
INTERFACE_PREFIX=$INTERFACE_PREFIX
INCOMING_INTERFACE=$INCOMING_INTERFACE
ROUTE_TABLE=$ROUTE_TABLE

# get the bridge IP where requests from host network will show up as
# There is probably a better way...
# TODO: fix this on aws (not needed for our primary testing cases; this is used for uncaptured to captured pod on same node)
HOST_IP="\$(ip addr show | grep 'inet.*veth' | head -n1 | tr -s ' ' | cut -d' ' -f3)"

## Prep:

# Setup a route table that sends all packets to 'lo' device
ip route del local 0.0.0.0/0 dev lo table $ROUTE_TABLE # potentially delete previous route
ip route add local 0.0.0.0/0 dev lo table $ROUTE_TABLE

# Route packets with tproxy mark locally so they end up with envoy.
# TODO: double check if this is needed, we maybe be able to just use tproxy rule.
ip rule del fwmark $MARK lookup $ROUTE_TABLE # potentially delete previous rule
ip rule add fwmark $MARK lookup $ROUTE_TABLE

# In the NAT table, accept stuff with our tproxy mark. This is done to we skip k8s NAT rules.
# Obviously needs to be inserted before k8s rules.
$IPTABLES -t nat -I uproxy-PREROUTING 1 -m mark --mark $MARK -j ACCEPT
$IPTABLES -t nat -I uproxy-PREROUTING 1 -m mark --mark $MARK -j LOG --log-prefix="[$node-mark] "



# move hack from init container to here, so all iptables are in one place. and use the same iptables (nft vs legacy)
# TODO(@yuval-k): @stevenctl should this be in the init container?
# we can't use tproxy here because it's on the output chain and tproxy only works in prerouting.
# we don't want to use REDIRECT, as it doesn't work with all CNIs. 
# so hopefully we will fix this soon by either using SNI, or even better, making this hop internal to envoy.
# Clean up previous rules, if any
$IPTABLES -t nat -D OUTPUT -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j REDIRECT --to-port $POD_INBOUND
$IPTABLES -t nat -D OUTPUT -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j LOG --log-prefix="[$node-internal] "
$IPTABLES -t nat -I OUTPUT 1 -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j REDIRECT --to-port $POD_INBOUND
$IPTABLES -t nat -I OUTPUT 1 -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j LOG --log-prefix="[$node-internal] "


## Original SRC setup (optional if original src is not desired).

# OUTMARK is set by envoy when doing original src. if we see packets with that mark, save it to the connection mark,
# so we can mark returning packets, as we need to divert them back to envoy.
# (we can also do this on the output chain).
$IPTABLES -t mangle -A uproxy-POSTROUTING -m mark --mark $OUTMARK -j LOG --log-prefix="[$node-save-conmark] "
$IPTABLES -t mangle -A uproxy-POSTROUTING -m mark --mark $OUTMARK -j CONNMARK --save-mark --nfmask $OUTMARK_MASK --ctmask $OUTMARK_MASK

# Alternativly, mark outgoing connections from envoy. we do need to exclude XDS if we do this.
# this has the advantage that no mark needs to be configured in envoy, and pilot and uproxy do not need to agree on a mark.
# TODO: make sure NEW doesn't include syn,ack.
# $IPTABLES -t mangle -A uproxy-POSTROUTING -m owner --uid-owner $ENVOY_UID -m conntrack --ctstate NEW -j LOG --log-prefix="[$node-conmark-out] "
# $IPTABLES -t mangle -A uproxy-POSTROUTING -m owner --uid-owner $ENVOY_UID -m conntrack --ctstate NEW -j CONNMARK --set-mark $OUTMARK --nfmask $OUTMARK_MASK --ctmask $OUTMARK_MASK

# If we see the connmark in the PREROUTING, this is a packet coming back and should be directed to envoy. mark it with $OUTMARK_RET and send it to envoy with an ip rule.
# We set a different mark on return path, as we only want these packages to go back to localhost, the outgoing packet needs to be routed to the local network card that belongs to the destination.
ip rule del fwmark $OUTMARK_RET lookup $ROUTE_TABLE # potentially delete previous rule
ip rule add fwmark $OUTMARK_RET lookup $ROUTE_TABLE
$IPTABLES -t mangle -A uproxy-PREROUTING -m connmark --mark $OUTMARK -j LOG --log-prefix="[$node-saw-conmark] "
$IPTABLES -t mangle -A uproxy-PREROUTING -m connmark --mark $OUTMARK -j MARK --set-mark $OUTMARK_RET
# if a packet has the return mark, accept it (as no futher processing is needed, and ip rule above will send it to envoy)
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $OUTMARK_RET -j LOG --log-prefix="[$node-return] "
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $OUTMARK_RET -j ACCEPT
# return here, so filter network policy will potentially be applied. TODO: test if it is really needed.
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $OUTMARK -j LOG --log-prefix="[$node-outmark] "
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $OUTMARK -j RETURN
$IPTABLES -t mangle -A uproxy-PREROUTING -m connmark --mark $SKIPMARK -j LOG --log-prefix="[$node-skipmark] "
$IPTABLES -t mangle -A uproxy-PREROUTING -m connmark --mark $SKIPMARK -j RETURN

## Pod membership and tproxy setup.

ipset destroy uproxy-pods-ips # potentially delete previous ipset
ipset create uproxy-pods-ips hash:ip
# Request is from uncaptured pod to captured pod on the same node. The request will make it *to* the pod without issue, but the packets coming back would be redirected
# Instead, we mark these and later skip them
# We match by interface and our ipset; this can probably be simplified
$IPTABLES -t mangle -A uproxy-POSTROUTING -p tcp \
  -o "${INTERFACE_PREFIX}+" -i "${INTERFACE_PREFIX}+" \
  -m set --match-set uproxy-pods-ips dst -m set ! --match-set uproxy-pods-ips src \
  -j LOG --log-prefix="[$node-skipmark] "
$IPTABLES -t mangle -A uproxy-POSTROUTING -p tcp \
  -o "${INTERFACE_PREFIX}+" -i "${INTERFACE_PREFIX}+" \
  -m set --match-set uproxy-pods-ips dst -m set ! --match-set uproxy-pods-ips src \
  -j CONNMARK --set-mark $SKIPMARK
# Also capture node host network which wouldn't match the -i argument above...
$IPTABLES -t mangle -A uproxy-POSTROUTING -p tcp \
  -o "${INTERFACE_PREFIX}+" \
  -m set --match-set uproxy-pods-ips dst -s \$HOST_IP \
  -j LOG --log-prefix="[$node-skipmarkh] "
$IPTABLES -t mangle -A uproxy-POSTROUTING -p tcp \
  -o "${INTERFACE_PREFIX}+" \
  -m set --match-set uproxy-pods-ips dst -s \$HOST_IP \
  -j CONNMARK --set-mark $SKIPMARK

# once we have a CNI the first tproxy rule can be changed to use an interface set. this has the advantage
# of allowing granual selection of which pods belong to uproxy and removes one configuration knob (the interface prefix).
# ipset create uproxy-pods-ifaces hash:iface

# note sure if needed. right now as the proxy sends traffic to itself if pod is on the same node
# $IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -d 127.0.0.1 --dport $POD_INBOUND -j ACCEPT
# disable rule above for now, as we have the nat rule in the init container

# TODO: once we sort out the pod in the same node communication, we can remove the " ! --dport 15088" part here.

# tproxy outbound connections from "injected" pods.
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" ! --dport 15088 -m set --match-set uproxy-pods-ips src -j LOG --log-prefix="[$node-outbound] "
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" ! --dport 15088 -m set --match-set uproxy-pods-ips src -j TPROXY --tproxy-mark $MARK --on-port $POD_OUTBOUND --on-ip 127.0.0.1

# if we got here, this is not an outbound connection from pod in the mesh.
# it could be an inbound connection from pod not in the mesh to a pod in the mesh.
# tproxy connections to "injected" pods (that can come from pods not in the mesh, but on the same node).
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" --dport 15008 -m set --match-set uproxy-pods-ips dst -j LOG --log-prefix="[$node-inbound] "
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" --dport 15008 -m set --match-set uproxy-pods-ips dst -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND --on-ip 127.0.0.1

# external traffic to pod
# tproxy connections coming from outside the node to "injected" pods.
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" -m set --match-set uproxy-pods-ips dst -j LOG --log-prefix="[$node-external in] "
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" -m set --match-set uproxy-pods-ips dst -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND_PLAINTEXT --on-ip 127.0.0.1
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INCOMING_INTERFACE}" ! --dport 15088 -m set --match-set uproxy-pods-ips dst -j LOG --log-prefix="[$node-external in2] "
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INCOMING_INTERFACE}" ! --dport 15088 -m set --match-set uproxy-pods-ips dst -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND_PLAINTEXT --on-ip 127.0.0.1

EOF
done
