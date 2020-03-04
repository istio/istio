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

# This script is the helper script to simulate an istio iptables setup on local machine.
sudo iptables -t nat -N ISTIO_IN_REDIRECT
sudo iptables -t nat -I ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15002
sudo iptables -t nat -I PREROUTING -p tcp -m tcp -d 127.0.0.6  -j ISTIO_IN_REDIRECT
sudo iptables -t nat -I OUTPUT  -d 127.0.0.6 -o lo -j ISTIO_IN_REDIRECT

ip -6 addr add 6::1/64 dev lo
sudo ip6tables -t nat -N ISTIO_IN_REDIRECT
sudo ip6tables -t nat -I ISTIO_IN_REDIRECT -p tcp -j REDIRECT --to-port 15002
sudo ip6tables -t nat -I PREROUTING -p tcp -m tcp -d ::6  -j ISTIO_IN_REDIRECT
sudo ip6tables -t nat -I OUTPUT  -d ::6 -o lo -j ISTIO_IN_REDIRECT

