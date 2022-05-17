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

for node in $(kind get nodes --name "${1:-kind}" | grep worker); do
  docker exec "$node" sh -c 'iptables-save | grep "^\-A" | grep "howardjohn" | cut -c 4- | xargs -r -L 1 iptables -t nat -D'
done
if [[ "${2:-}" == clean ]]; then
  exit 0
fi
for node in $(kind get nodes --name "${1:-kind}" | grep worker); do
  docker exec "$node" iptables -t nat -I PREROUTING 1 -p tcp -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node preroute] "
  docker exec "$node" iptables -t nat -I OUTPUT 1 -p tcp -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node output] "
  docker exec "$node" iptables -t nat -I INPUT 1 -p tcp -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node input] "
echo
done

for node in $(kind get nodes --name "${1:-kind}" | grep worker); do
  docker exec "$node" iptables -t nat -I PREROUTING 1 -p tcp -i eth0 -m comment --comment "howardjohn"  -j REDIRECT --to-port 15006
  docker exec "$node" iptables -t nat -I PREROUTING 1 -p tcp --dport 15008 -i eth0 -m comment --comment "howardjohn" -j REDIRECT --to-port 15008
  docker exec "$node" iptables -t nat -I PREROUTING 1 -p tcp -i eth0 -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node POD INBOUND] "
  docker exec "$node" iptables -t nat -I PREROUTING 1 -p tcp -i veth+ -m comment --comment "howardjohn" -j REDIRECT --to-port 15001
  docker exec "$node" iptables -t nat -I PREROUTING 1 -p tcp -i veth+ -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node POD OUTBOUND] "
  docker exec "$node" iptables -t nat -I OUTPUT 1 -p tcp -o eth0 -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node NODE REMOTE OUTBOUND] "
  docker exec "$node" iptables -t nat -I OUTPUT 1 -p tcp -o veth+ --dport 15008 -m comment --comment "howardjohn"   -j REDIRECT --to-port 15008
  docker exec "$node" iptables -t nat -I OUTPUT 1 -p tcp -o veth+ -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node NODE LOCAL OUTBOUND] "
  #docker exec $node iptables -t nat -I OUTPUT 1 -p tcp -o veth+ -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node NODE LOCAL] "
  #docker exec $node iptables -t nat -I INPUT 1 -p tcp -i veth+ -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node UPROXY INBOUND] "
  #docker exec $node iptables -t nat -I INPUT 1 -p tcp -i eth0 -m comment --comment "howardjohn"  -j LOG --log-prefix="[$node NODE INBOUND] "
  echo
done