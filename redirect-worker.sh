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

# Allocate marks and route tables:
# the main requirement here is that these won't class with other things running on the host.
# note that the outmark needs to be configured by pilot as well, so envoy sets it on outgoing traffic.
# though we can potentially avoid using it and user UID exclusions instead.
# TPROXY port in envoy
POD_OUTBOUND=15001
POD_INBOUND=15008
POD_INBOUND_PLAINTEXT=15006

if $IPTABLES -t mangle -C OUTPUT -j uproxy-OUTPUT; then
  $IPTABLES -t nat -F uproxy-PREROUTING
  $IPTABLES -t nat -F uproxy-POSTROUTING
  $IPTABLES -t mangle -F uproxy-PREROUTING
  $IPTABLES -t mangle -F uproxy-OUTPUT
  $IPTABLES -t mangle -F uproxy-FORWARD
  $IPTABLES -t mangle -F uproxy-INPUT
else
  $IPTABLES -t nat -N uproxy-PREROUTING
  $IPTABLES -t nat -I PREROUTING -j uproxy-PREROUTING
  $IPTABLES -t nat -N uproxy-POSTROUTING
  $IPTABLES -t nat -I POSTROUTING -j uproxy-POSTROUTING
  $IPTABLES -t mangle -N uproxy-PREROUTING
  $IPTABLES -t mangle -I PREROUTING -j uproxy-PREROUTING
  $IPTABLES -t mangle -N uproxy-OUTPUT
  $IPTABLES -t mangle -I OUTPUT -j uproxy-OUTPUT
  $IPTABLES -t mangle -N uproxy-FORWARD
  $IPTABLES -t mangle -I FORWARD -j uproxy-FORWARD
  $IPTABLES -t mangle -N uproxy-INPUT
  $IPTABLES -t mangle -I INPUT -j uproxy-INPUT
fi 

# try host side veth
HOST_IP="$(ip addr show | grep 'inet.*veth' | head -n1 | awk '{print $2}' | cut -d/ -f1)"
if [ -z "$HOST_IP" ]; then
  # try eth0
  HOST_IP=$(ip addr show eth0 | grep "inet\b" | awk '{print $2}' | cut -d/ -f1)
fi


# anything coming from uproxy veth needs to be skipped, regardless of what ip it uses
UPROXYID=$(crictl ps -o json | jq '.containers[] | select(.labels."io.kubernetes.pod.name" | startswith("uproxy-")) | select(.labels."io.kubernetes.pod.namespace" == "istio-system") | .id' -r)
UPROXY_IP=$(crictl exec $UPROXYID ip addr show eth0 | grep "inet\b" | awk '{print $2}' | cut -d/ -f1)
UPROXY_VETH=$(ip route get $UPROXY_IP | sed -nr 's/.* dev ([[:alnum:]]+) .*/\1/p')
# In host ns:
# everything with skip mark goes directly to the main table.
ip rule add priority $(($IP_RULE_BASE+0)) fwmark $SKIP_MARK goto $IP_RULE_GOTO
# everything with outbound mark, goes to the tunnel out device using the outbound route table
ip rule add priority $(($IP_RULE_BASE+1)) fwmark $OUTBOUND_MARK lookup $OUTBOUND_ROUTE_TABLE
# things with proxy return mark go directly to the proxy veth using the proxy route table (i.e. useful for original src)
ip rule add priority $(($IP_RULE_BASE+2)) fwmark $PROXY_RET_MARK lookup $PROXY_ROUTE_TABLE

# send all traffic to the inbound table. this table has routes only to pods in the mesh.
# this table doesn't have a catch-all route. if a route is missing, the search will continue.
# this allows us to "override" routing just for member pods.

ip rule add priority $(($IP_RULE_BASE+4)) goto $(($IP_RULE_BASE+7))
ip rule add priority $(($IP_RULE_BASE+5)) table $INBOUND_ROUTE_TABLE2
ip rule add priority $(($IP_RULE_BASE+6)) goto $IP_RULE_GOTO
ip rule add priority $(($IP_RULE_BASE+7)) table $INBOUND_ROUTE_TABLE


# create the set of pod members
IPSET=ipset
if [ "${K8S_TYPE}" == gke ]; then
  IPSET="toolbox ipset"
fi
$IPSET create uproxy-pods-ips hash:ip

# skip things that come from the tunnels, but don't apply the conn skip mark, so only this flow is skipped
$IPTABLES -t mangle -A uproxy-PREROUTING -i $INBOUND_TUN -j MARK --set-mark $SKIP_MARK
$IPTABLES -t mangle -A uproxy-PREROUTING -i $INBOUND_TUN -j RETURN
$IPTABLES -t mangle -A uproxy-PREROUTING -i $OUTBOUND_TUN -j MARK --set-mark $SKIP_MARK
$IPTABLES -t mangle -A uproxy-PREROUTING -i $OUTBOUND_TUN -j RETURN

# make sure that whatever is skipped, also is skipped for returning packets:
# if we have a skip mark, save it to conn mark.
$IPTABLES -t mangle -A uproxy-FORWARD -m mark --mark $CONNSKIP_MARK -j CONNMARK --save-mark --nfmask $CONNSKIP_MASK --ctmask $CONNSKIP_MASK
# input chain might be needed for things in host ns that are skipped.
# i place the mark here after routing was done, as i'm not sure if conn-tracking will figure
# it out if i do it before, as NAT might change the connection tuple.
$IPTABLES -t mangle -A uproxy-INPUT -m mark --mark $CONNSKIP_MARK -j CONNMARK --save-mark --nfmask $CONNSKIP_MASK --ctmask $CONNSKIP_MASK

# For things we the proxy mark, we need different routing just on returning packets.
# so we give a different mark to the returning packets.
$IPTABLES -t mangle -A uproxy-FORWARD -m mark --mark $PROXY_MARK -j CONNMARK --save-mark --nfmask $PROXY_MASK --ctmask $PROXY_MASK
$IPTABLES -t mangle -A uproxy-INPUT -m mark --mark $PROXY_MARK -j CONNMARK --save-mark --nfmask $PROXY_MASK --ctmask $PROXY_MASK

$IPTABLES -t mangle -A uproxy-OUTPUT --source $HOST_IP -j MARK --set-mark $CONNSKIP_MARK

# disable for now (as i'm not sure if needed), but if you see `martian` packets in the logs, renable this.
# # allow original src to go through
echo 0 > /proc/sys/net/ipv4/conf/all/rp_filter # need to set all as well, as the max is taken.
echo 0 > /proc/sys/net/ipv4/conf/$UPROXY_VETH/rp_filter
echo 1 > /proc/sys/net/ipv4/conf/$UPROXY_VETH/accept_local

# disable rp filter everywhere, as its needed on AWS
echo 0 > /proc/sys/net/ipv4/conf/default/rp_filter
for i in /proc/sys/net/ipv4/conf/*/rp_filter; do
  echo 0 > $i
done

# If we have an outbound mark, we don't need kube-proxy to do anything, so accept it before kube proxy
# translates service vips to pod ips.
$IPTABLES -t nat -A uproxy-PREROUTING -m mark --mark $OUTBOUND_MARK -j ACCEPT
$IPTABLES -t nat -A uproxy-POSTROUTING -m mark --mark $OUTBOUND_MARK -j ACCEPT

# don't set anything on the tunnel (geneve port is 6081), as the tunnel copies the mark to the un-tunneled packet.
$IPTABLES -t mangle -A uproxy-PREROUTING -p udp -m udp --dport 6081 -j RETURN

# if we have the conn mark, restore it to mark, to make sure that the other side of the connection is skipped as well.
$IPTABLES -t mangle -A uproxy-PREROUTING -m connmark --mark $CONNSKIP_MARK -j MARK --set-mark $SKIP_MARK
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $SKIP_MARK -j RETURN

# if we have the proxy mark in, set the return mark, to make sure that original src packets go to uproxy.
$IPTABLES -t mangle -A uproxy-PREROUTING ! -i $UPROXY_VETH -m connmark --mark $PROXY_MARK -j MARK --set-mark $PROXY_RET_MARK
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $PROXY_RET_MARK -j RETURN

# send fake source outbound connections to the outbound route table (for original src)
# if it's original src, the source ip of packets coming from the proxy might be that of a pod, so 
# make sure we don't tproxy it.
$IPTABLES -t mangle -A uproxy-PREROUTING -i $UPROXY_VETH ! --source $UPROXY_IP -j MARK --set-mark $PROXY_MARK
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $SKIP_MARK -j RETURN

# make sure anything that leaves the uproxy is routed normally (xds, connections to other uproxies, connections to upstream pods...) .
$IPTABLES -t mangle -A uproxy-PREROUTING -i $UPROXY_VETH -j MARK --set-mark $CONNSKIP_MARK

# skip udp, so DNS works. we can make this more granular.
$IPTABLES -t mangle -A uproxy-PREROUTING -p udp -j MARK --set-mark $CONNSKIP_MARK
# skip things from the host ip - these are usually kubectl probes
# skip anything with skip mark. this can be used to add features like port exclusions.
$IPTABLES -t mangle -A uproxy-PREROUTING -m mark --mark $SKIP_MARK -j RETURN
# mark outbound connections, this will route them to the proxy using ip rules/route tables.
$IPTABLES -t mangle -A uproxy-PREROUTING -p tcp -i "${INTERFACE_PREFIX}+" -m set --match-set uproxy-pods-ips src -j MARK --set-mark $OUTBOUND_MARK

# create host side tunnels
ip link add name $INBOUND_TUN type geneve id 1000 remote $UPROXY_IP
ip addr add $INBOUND_TUN_IP/$TUN_PREFIX dev $INBOUND_TUN

ip link add name $OUTBOUND_TUN type geneve id 1001 remote $UPROXY_IP
ip addr add $OUTBOUND_TUN_IP/$TUN_PREFIX dev $OUTBOUND_TUN

ip link set $INBOUND_TUN up
ip link set $OUTBOUND_TUN up

# route uproxy ip (i.e. geneve remote) to the uproxy veth directly. needed so geneve doesn't go into a loop
ip route add table $OUTBOUND_ROUTE_TABLE $UPROXY_IP dev $UPROXY_VETH scope link
# anything else in the outbound route table just goes to the outbound tunnel
ip route add table $OUTBOUND_ROUTE_TABLE 0.0.0.0/0 via $UPROXY_OUTBOUND_TUN_IP dev $OUTBOUND_TUN
# handle original src, by sending original src traffic to the uproxy veth...
ip route add table $PROXY_ROUTE_TABLE $UPROXY_IP dev $UPROXY_VETH scope link
ip route add table $PROXY_ROUTE_TABLE 0.0.0.0/0 via $UPROXY_IP dev $UPROXY_VETH onlink
# same for inbound table. in theory i don't think i should need this, but didn't work without.
ip route add table $INBOUND_ROUTE_TABLE $UPROXY_IP dev $UPROXY_VETH scope link


echo 0 > /proc/sys/net/ipv4/conf/$INBOUND_TUN/rp_filter
echo 1 > /proc/sys/net/ipv4/conf/$INBOUND_TUN/accept_local
echo 0 > /proc/sys/net/ipv4/conf/$OUTBOUND_TUN/rp_filter
echo 1 > /proc/sys/net/ipv4/conf/$OUTBOUND_TUN/accept_local

################################ UPROXY #################################
# all this stuff below should ideally be in the uproxy init container, but for now it's easier to do it here.
# as we can share the config

# del potentially previous ifaces
# add a 'p' prefix so it's easier to debug. it's not really needed.
crictl exec $UPROXYID ip link del p$INBOUND_TUN
crictl exec $UPROXYID ip link del p$OUTBOUND_TUN

# create a tunnel. note the tunnel ids need to match the host side ones.
crictl exec $UPROXYID ip link add name p$INBOUND_TUN type geneve id 1000 remote $HOST_IP
crictl exec $UPROXYID ip addr add $UPROXY_INBOUND_TUN_IP/$TUN_PREFIX dev p$INBOUND_TUN

crictl exec $UPROXYID ip link add name p$OUTBOUND_TUN type geneve id 1001 remote $HOST_IP
crictl exec $UPROXYID ip addr add $UPROXY_OUTBOUND_TUN_IP/$TUN_PREFIX dev p$OUTBOUND_TUN

crictl exec $UPROXYID ip link set p$INBOUND_TUN up
crictl exec $UPROXYID ip link set p$OUTBOUND_TUN up

# note that because the fw mark is local to the pod, i don't have to use the constants above.
# these needs to match the mark constants in uproxygen.go
PROXY_OUTBOUND_MARK=0x401/0xfff
PROXY_INBOUND_MARK=0x402/0xfff
PROXY_ORG_SRC_MARK=0x4d2/0xfff
# tproxy mark, it's only used here.
MARK=0x400/0xfff
ORG_SRC_RET_MARK=0x4d3/0xfff
crictl exec $UPROXYID ip rule del priority 20000
crictl exec $UPROXYID ip rule del priority 20001
crictl exec $UPROXYID ip rule del priority 20002
crictl exec $UPROXYID ip rule del priority 20003

crictl exec $UPROXYID ip route flush table 100
crictl exec $UPROXYID ip route flush table 101
crictl exec $UPROXYID ip route flush table 102

crictl exec $UPROXYID ip rule add priority 20000 fwmark $MARK lookup 100
crictl exec $UPROXYID ip rule add priority 20001 fwmark $PROXY_OUTBOUND_MARK lookup 101
crictl exec $UPROXYID ip rule add priority 20002 fwmark $PROXY_INBOUND_MARK lookup 102
crictl exec $UPROXYID ip rule add priority 20003 fwmark $ORG_SRC_RET_MARK lookup 100
crictl exec $UPROXYID ip route add local 0.0.0.0/0 dev lo table 100

crictl exec $UPROXYID ip route add table 101 $HOST_IP dev eth0 scope link
crictl exec $UPROXYID ip route add table 101 0.0.0.0/0 via $OUTBOUND_TUN_IP dev p$OUTBOUND_TUN

crictl exec $UPROXYID ip route add table 102 $HOST_IP dev eth0 scope link
crictl exec $UPROXYID ip route add table 102 0.0.0.0/0 via $INBOUND_TUN_IP dev p$INBOUND_TUN

crictl exec $UPROXYID $IPTABLES -t mangle -F PREROUTING # clean up previous rules to make script idempotent
crictl exec $UPROXYID $IPTABLES -t nat -F OUTPUT # clean up previous rules to make script idempotent
# is it from another uproxy?
crictl exec $UPROXYID $IPTABLES -t mangle -A PREROUTING -p tcp -i p$INBOUND_TUN -m tcp --dport=$POD_INBOUND -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND --on-ip 127.0.0.1
crictl exec $UPROXYID $IPTABLES -t mangle -A PREROUTING -p tcp -i p$OUTBOUND_TUN -j TPROXY --tproxy-mark $MARK --on-port $POD_OUTBOUND --on-ip 127.0.0.1
crictl exec $UPROXYID $IPTABLES -t mangle -A PREROUTING -p tcp -i p$INBOUND_TUN -j TPROXY --tproxy-mark $MARK --on-port $POD_INBOUND_PLAINTEXT --on-ip 127.0.0.1
# mark original src - packets coming from main interface, but not for us:
crictl exec $UPROXYID $IPTABLES -t mangle -A PREROUTING -p tcp -i eth0 ! --dst $UPROXY_IP -j MARK --set-mark $ORG_SRC_RET_MARK

# set the mark here in case there's original src..
# crictl exec $UPROXYID $IPTABLES -t nat -A OUTPUT -p tcp --dport 15088 -j MARK --set-mark $ORG_SRC_RET_MARK
crictl exec $UPROXYID $IPTABLES -t nat -A OUTPUT -p tcp --dport 15088 -j REDIRECT --to-port $POD_INBOUND