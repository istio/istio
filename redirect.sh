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
# prefix for pod network interfaces on the host side
INTERFACE_PREFIX=veth
INCOMING_INTERFACE=eth0
# a route table number number we can use to send traffic to envoy (should be unused).
ROUTE_TABLE=100

if [[ "${2:-}" == clean ]]; then
  # clean up previous chains
  for node in $(kind get nodes --name "${1:-kind}" | grep worker); do
    docker exec -i "$node" sh -x <<EOF
    iptables-nft -t nat -F uproxy-PREROUTING
    iptables-nft -t mangle -F uproxy-PREROUTING
    iptables-nft -t mangle -F uproxy-POSTROUTING
    # we flush because sometimes -X doesn't work.
    iptables-nft -t mangle -D POSTROUTING -j uproxy-POSTROUTING
    iptables-nft -t mangle -X uproxy-POSTROUTING
    iptables-nft -t mangle -D PREROUTING -j uproxy-PREROUTING
    iptables-nft -t mangle -X uproxy-PREROUTING
    iptables-nft -t nat -D PREROUTING -j uproxy-PREROUTING
    iptables-nft -t nat -X uproxy-PREROUTING
    ip route del local 0.0.0.0/0 dev lo table $ROUTE_TABLE
    ipset destroy uproxy-pods-ips
EOF
  done

  exit 0
fi


for node in $(kind get nodes --name "${1:-kind}" | grep worker); do
docker exec -i "$node" sh <<EOF
  if ! command -v ipset; then
    "$node" apt update
    "$node" apt install ipset -y
  fi
EOF
done

# add our tables if not exist yet, flush them if they do exist.
for node in $(kind get nodes --name "${1:-kind}" | grep worker); do

docker exec -i "$node" sh -x <<EOF
if iptables-nft -t nat -C PREROUTING -j uproxy-PREROUTING; then
  iptables-nft -t nat -F uproxy-PREROUTING
  iptables-nft -t mangle -F uproxy-PREROUTING
  iptables-nft -t mangle -F uproxy-POSTROUTING
else 
  iptables-nft -t nat -N uproxy-PREROUTING
  iptables-nft -t nat -I PREROUTING -j uproxy-PREROUTING
  iptables-nft -t mangle -N uproxy-PREROUTING
  iptables-nft -t mangle -I PREROUTING -j uproxy-PREROUTING
  iptables-nft -t mangle -N uproxy-POSTROUTING
  iptables-nft -t mangle -I POSTROUTING -j uproxy-POSTROUTING
fi 
EOF
done

# create our rules
for node in $(kind get nodes --name "${1:-kind}" | grep worker); do
docker exec -i "$node" sh -x <<EOF
# copy the env vars in
POD_OUTBOUND=$POD_OUTBOUND
POD_INBOUND=$POD_INBOUND
MARK_MASK=$MARK_MASK
MARK=$MARK
OUTMARK_MASK=$OUTMARK_MASK
OUTMARK=$OUTMARK
OUTMARK_RET_MASK=$OUTMARK_RET_MASK
OUTMARK_RET=$OUTMARK_RET
INTERFACE_PREFIX=$INTERFACE_PREFIX
INCOMING_INTERFACE=$INCOMING_INTERFACE
ROUTE_TABLE=$ROUTE_TABLE

## Prep:

# Setup a route table that sends all packets to 'lo' device
ip route add local 0.0.0.0/0 dev lo table $ROUTE_TABLE

# Route packets with tproxy mark locally so they end up with envoy.
# TODO: double check if this is needed, we maybe be able to just use tproxy rule.
ip rule add fwmark $MARK lookup $ROUTE_TABLE

# In the NAT table, accept stuff with our tproxy mark. This is done to we skip k8s NAT rules.
# Obviously needs to be inserted before k8s rules.
iptables-nft -t nat -I uproxy-PREROUTING 1 -m mark --mark $MARK -j ACCEPT

## Original SRC setup (optional if original src is not desired).

# OUTMARK is set by envoy when doing original src. if we see packets with that mark, save it to the connection mark,
# so we can mark returning packets, as we need to divert them back to envoy.
# (we can also do this on the output chain).
iptables-nft -t mangle -A uproxy-POSTROUTING -o "${INTERFACE_PREFIX}+" -m mark --mark $OUTMARK -j CONNMARK --save-mark --nfmask $OUTMARK_MASK --ctmask $OUTMARK_MASK

# Alternativly, mark outgoing connections from envoy. we do need to exclude XDS if we do this.
# this has the advantage that no mark needs to be configured in envoy, and pilot and uproxy do not need to agree on a mark.
# TODO: make sure NEW doesn't include syn,ack.
# iptables-nft -t mangle -A uproxy-POSTROUTING -o "${INTERFACE_PREFIX}+" -m owner --uid-owner $ENVOY_UID -m conntrack --ctstate NEW -j CONNMARK --set-mark $OUTMARK --nfmask $OUTMARK_MASK --ctmask $OUTMARK_MASK

# move hack from init container to here, so all iptables are in one place. and use the same iptables (nft vs legacy)
# TODO(@yuval-k): @stevenctl should this be in the init container?
# we can't use tproxy here because it's on the output chain and tproxy only works in prerouting.
# we don't want to use REDIRECT, as it doesn't work with all CNIs. 
# so hopefully we will fix this soon by either using SNI, or even better, making this hop internal to envoy.
iptables-nft -t nat -I OUTPUT 1 -p tcp -o "${INTERFACE_PREFIX}+" --dport 15088 -j REDIRECT --to-port $POD_INBOUND

# If we see the connmark in the PREROUTING, this is a packet coming back and should be directed to envoy. mark it with $OUTMARK_RET and send it to envoy with an ip rule.
# We set a different mark on return path, as we only want these packages to go back to localhost, the outgoing packet needs to be routed to the local network card that belongs to the destination.
ip rule add fwmark $OUTMARK_RET lookup $ROUTE_TABLE
iptables-nft -t mangle -A uproxy-PREROUTING -i "${INTERFACE_PREFIX}+" -m connmark --mark $OUTMARK -j MARK --set-mark $OUTMARK_RET --nfmask $OUTMARK_RET_MASK --ctmask $OUTMARK_MASK
# if a packet has the return mark, accept it (as no futher processing is needed, and ip rule above will send it to envoy)
iptables-nft -t mangle -A uproxy-PREROUTING -i "${INTERFACE_PREFIX}+" -m mark --mark $OUTMARK_RET -j ACCEPT
# return here, so filter network policy will potentially be applied. TODO: test if it is really needed.
iptables-nft -t mangle -A uproxy-PREROUTING -i "${INTERFACE_PREFIX}+" -m mark --mark $OUTMARK -j RETURN

ipset create uproxy-pods-ips hash:ip
# once we have a CNI the first tproxy rule can be changed to use an interface set. this has the advantage
# of allowing granual selection of which pods belong to uproxy and removes one configuration knob (the interface prefix).
# ipset create uproxy-pods-ifaces hash:iface

# note sure if needed. right now as the proxy sends traffic to itself if pod is on the same node
# iptables-nft -t mangle -A uproxy-PREROUTING -p tcp -d 127.0.0.1 --dport $POD_INBOUND -j ACCEPT
# disable rule above for now, as we have the nat rule in the init container

# TODO: once we sort out the pod in the same node communication, we can remove the " ! --dport 15088" part here.

# tproxy outbound connections from "injected" pods.
iptables-nft -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" ! --dport 15088 -m set --match-set uproxy-pods-ips src -j TPROXY --tproxy-mark $MARK --on-port $POD_OUTBOUND --on-ip 127.0.0.1

# if we got here, this is not an outbound connection from pod in the mesh.
# it could be an inbound connection from pod not in the mesh to a pod in the mesh.
# tproxy connections to "injected" pods (that can come from pods not in the mesh, but on the same node).
iptables-nft -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" ! --dport 15088 -m set --match-set uproxy-pods-ips dst -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND --on-ip 127.0.0.1

# external traffic to pod
# tproxy connections coming from outside the node to "injected" pods.
iptables-nft -t mangle -A uproxy-PREROUTING -p tcp -i "${INCOMING_INTERFACE}" ! --dport 15088 -m set --match-set uproxy-pods-ips dst -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND --on-ip 127.0.0.1

EOF
done
