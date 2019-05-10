#!/bin/bash
#
# Copyright 2019 Istio Authors. All Rights Reserved.
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
#
################################################################################

function assert_equals() {
    if [ "$1" != "$2" ]; then
        echo "Expected result "
        echo $1
        echo "doesn't match current result"
        echo $2
        exit 1
    fi
}

TEST_FILE=/tmp/istio-iptables.sh
TEST_SHELL=/usr/local/bin/bash
cat > $TEST_FILE <<-END
#!$TEST_SHELL
iptables() {
    echo "iptables \$*"
}

ip6tables() {
    echo  "ip6tables \$*"
}

ip() {
    echo  "ip \$*"
}

hostname() {
    echo "127.0.0.1"
}

id() {
    echo "0"
}

END
grep -v "^trap" ./tools/packaging/common/istio-iptables.sh >> $TEST_FILE


# Test mode REDIRECT
OUTPUT=$($TEST_SHELL $TEST_FILE -p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888  -i 1.1.1.0/16 -x 9.9.9.0/16  -k eth1,eth2  2>/dev/null)
EXPECTED_OUTPUT=$(cat <<-END
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
Environment:
------------
ENVOY_PORT=
ISTIO_INBOUND_INTERCEPTION_MODE=
ISTIO_INBOUND_TPROXY_MARK=
ISTIO_INBOUND_TPROXY_ROUTE_TABLE=
ISTIO_INBOUND_PORTS=
ISTIO_LOCAL_EXCLUDE_PORTS=
ISTIO_SERVICE_CIDR=
ISTIO_SERVICE_EXCLUDE_CIDR=

Variables:
----------
PROXY_PORT=12345
INBOUND_CAPTURE_PORT=12345
PROXY_UID=4321
INBOUND_INTERCEPTION_MODE=REDIRECT
INBOUND_TPROXY_MARK=1337
INBOUND_TPROXY_ROUTE_TABLE=133
INBOUND_PORTS_INCLUDE=5555,6666
INBOUND_PORTS_EXCLUDE=7777,8888
OUTBOUND_IP_RANGES_INCLUDE=1.1.1.0/16
OUTBOUND_IP_RANGES_EXCLUDE=9.9.9.0/16
KUBEVIRT_INTERFACES=eth1,eth2
ENABLE_INBOUND_IPV6=

iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_INBOUND
iptables -t,nat,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-j,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-j,ISTIO_IN_REDIRECT
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,9.9.9.0/16,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-d,1.1.1.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-d,1.1.1.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-d,1.1.1.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
END
)

assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"


# Test mode TPROXY
OUTPUT=$($TEST_SHELL $TEST_FILE -p 12345 -u 4321 -g 4444 -m TPROXY -b 5555,6666 -d 7777,8888  -i 1.1.1.0/16 -x 9.9.9.0/16  -k eth1,eth2 2>/dev/null)
EXPECTED_OUTPUT=$(cat <<-END
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
Environment:
------------
ENVOY_PORT=
ISTIO_INBOUND_INTERCEPTION_MODE=
ISTIO_INBOUND_TPROXY_MARK=
ISTIO_INBOUND_TPROXY_ROUTE_TABLE=
ISTIO_INBOUND_PORTS=
ISTIO_LOCAL_EXCLUDE_PORTS=
ISTIO_SERVICE_CIDR=
ISTIO_SERVICE_EXCLUDE_CIDR=

Variables:
----------
PROXY_PORT=12345
INBOUND_CAPTURE_PORT=12345
PROXY_UID=4321
INBOUND_INTERCEPTION_MODE=TPROXY
INBOUND_TPROXY_MARK=1337
INBOUND_TPROXY_ROUTE_TABLE=133
INBOUND_PORTS_INCLUDE=5555,6666
INBOUND_PORTS_EXCLUDE=7777,8888
OUTBOUND_IP_RANGES_INCLUDE=1.1.1.0/16
OUTBOUND_IP_RANGES_EXCLUDE=9.9.9.0/16
KUBEVIRT_INTERFACES=eth1,eth2
ENABLE_INBOUND_IPV6=

iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,12345
iptables -t,mangle,-N,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_DIVERT,-j,MARK,--set-mark,1337
iptables -t,mangle,-A,ISTIO_DIVERT,-j,ACCEPT
ip -f,inet,rule,add,fwmark,1337,lookup,133
ip -f,inet,route,add,local,default,dev,lo,table,133
iptables -t,mangle,-N,ISTIO_TPROXY
iptables -t,mangle,-A,ISTIO_TPROXY,!,-d,127.0.0.1/32,-p,tcp,-j,TPROXY,--tproxy-mark,1337/0xffffffff,--on-port,12345
iptables -t,mangle,-N,ISTIO_INBOUND
iptables -t,mangle,-A,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,5555,-j,ISTIO_TPROXY
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-m,socket,-j,ISTIO_DIVERT
iptables -t,mangle,-A,ISTIO_INBOUND,-p,tcp,--dport,6666,-j,ISTIO_TPROXY
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,4321,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,4444,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,9.9.9.0/16,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-j,RETURN
iptables -t,nat,-I,PREROUTING,1,-i,eth1,-d,1.1.1.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-I,PREROUTING,1,-i,eth2,-d,1.1.1.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-d,1.1.1.0/16,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
END
)

assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"

# Test empty parameter
OUTPUT=$($TEST_SHELL $TEST_FILE 2>/dev/null)
EXPECTED_OUTPUT=$(cat <<-END
iptables -t,nat,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,mangle,-D,PREROUTING,-p,tcp,-j,ISTIO_INBOUND
iptables -t,nat,-D,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_OUTPUT
iptables -t,nat,-X,ISTIO_OUTPUT
iptables -t,nat,-F,ISTIO_INBOUND
iptables -t,nat,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_INBOUND
iptables -t,mangle,-X,ISTIO_INBOUND
iptables -t,mangle,-F,ISTIO_DIVERT
iptables -t,mangle,-X,ISTIO_DIVERT
iptables -t,mangle,-F,ISTIO_TPROXY
iptables -t,mangle,-X,ISTIO_TPROXY
iptables -t,nat,-F,ISTIO_REDIRECT
iptables -t,nat,-X,ISTIO_REDIRECT
iptables -t,nat,-F,ISTIO_IN_REDIRECT
iptables -t,nat,-X,ISTIO_IN_REDIRECT
Environment:
------------
ENVOY_PORT=
ISTIO_INBOUND_INTERCEPTION_MODE=
ISTIO_INBOUND_TPROXY_MARK=
ISTIO_INBOUND_TPROXY_ROUTE_TABLE=
ISTIO_INBOUND_PORTS=
ISTIO_LOCAL_EXCLUDE_PORTS=
ISTIO_SERVICE_CIDR=
ISTIO_SERVICE_EXCLUDE_CIDR=

Variables:
----------
PROXY_PORT=15001
INBOUND_CAPTURE_PORT=15001
PROXY_UID=0,0
INBOUND_INTERCEPTION_MODE=
INBOUND_TPROXY_MARK=1337
INBOUND_TPROXY_ROUTE_TABLE=133
INBOUND_PORTS_INCLUDE=
INBOUND_PORTS_EXCLUDE=
OUTBOUND_IP_RANGES_INCLUDE=
OUTBOUND_IP_RANGES_EXCLUDE=
KUBEVIRT_INTERFACES=
ENABLE_INBOUND_IPV6=

iptables -t,nat,-N,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,15001
iptables -t,nat,-N,ISTIO_IN_REDIRECT
iptables -t,nat,-A,ISTIO_IN_REDIRECT,-p,tcp,-j,REDIRECT,--to-port,15001
iptables -t,nat,-N,ISTIO_OUTPUT
iptables -t,nat,-A,OUTPUT,-p,tcp,-j,ISTIO_OUTPUT
iptables -t,nat,-A,ISTIO_OUTPUT,-o,lo,!,-d,127.0.0.1/32,-j,ISTIO_REDIRECT
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,0,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--uid-owner,0,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,0,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-m,owner,--gid-owner,0,-j,RETURN
iptables -t,nat,-A,ISTIO_OUTPUT,-d,127.0.0.1/32,-j,RETURN
ip6tables -F,INPUT
ip6tables -A,INPUT,-m,state,--state,ESTABLISHED,-j,ACCEPT
ip6tables -A,INPUT,-i,lo,-d,::1,-j,ACCEPT
ip6tables -A,INPUT,-j,REJECT
END
)

assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"

echo "Test was sucessful"