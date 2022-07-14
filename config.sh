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

# socket mark setup
OUTBOUND_MASK="0x100"
OUTBOUND_MARK="0x100/$OUTBOUND_MASK"

SKIP_MASK="0x200"
SKIP_MARK="0x200/$SKIP_MASK"

# note!! this includes the skip mark bit, so match on skip mark will match this as well.
CONNSKIP_MASK="0x220"
CONNSKIP_MARK="0x220/$CONNSKIP_MASK"

# note!! this includes the skip mark bit, so match on skip mark will match this as well.
PROXY_MASK="0x210"
PROXY_MARK="0x210/$PROXY_MASK"

PROXY_RET_MASK="0x040"
PROXY_RET_MARK="0x040/$PROXY_RET_MASK"

# prefix for pod network interfaces on the host side
INTERFACE_PREFIX=veth
INBOUND_TUN=istioin
OUTBOUND_TUN=istioout

# TODO: look into why link local (169.254.x.x) address didn't work
# they don't respond to ARP.
INBOUND_TUN_IP=192.168.126.1
UPROXY_INBOUND_TUN_IP=192.168.126.2
OUTBOUND_TUN_IP=192.168.127.1
UPROXY_OUTBOUND_TUN_IP=192.168.127.2
TUN_PREFIX=30

# a route table number number we can use to send traffic to envoy (should be unused).
INBOUND_ROUTE_TABLE=100
INBOUND_ROUTE_TABLE2=103
OUTBOUND_ROUTE_TABLE=101
# needed for original src.
PROXY_ROUTE_TABLE=102

IP_RULE_BASE=100
IP_RULE_GOTO=32766
