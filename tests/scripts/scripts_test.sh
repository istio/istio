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

#
# Testing istio-iptables.sh  ip address validations
#
function fail_test {
    echo "Test case: $1 failed for function $2 while testing address: $3"
    exit 1
}

echo "Running IPv6 and IPv4 addresses validation functions..."
set -e
. ./tools/packaging/common/istio-iptables.sh -t
echo "Validation functions are loaded"
declare -a test_good_ipv4
declare -a test_bad_ipv4
declare -a test_good_ipv6
declare -a test_bad_ipv4
# list of good ipv4 addresses
test_good_ipv4=("1.1.1.1" "127.0.0.1" "11.111.11.1" "111.111.111.111")
# list of invalid ipv4 addresses
test_bad_ipv4=(".1.1.1" "127.0.a.1" "1111.111.11.1" "111.111.111")
# list of good ipv6 addresses
test_good_ipv6=("2001:db8:1::1" "fe80::1" "fe80::5054:ff:fe6c:1c4d" "2001:470:b16e:81::11" "1111:a2a2:b3b3:c4c4:d5d5:e5e5:f6f6:a8a8")
# list of invalid ipv6 addresses
test_bad_ipv6=("" "::1" "1111:b2b21::1" "1111:ab2g::1" "1111:2222:3333:::1" "1111:2222:3333:4444:5555:6666:7777:8888:1")

# Testing valid ipv4 cases
for addr in "${test_good_ipv4[@]}"; do
    if ! isValidIP "$addr"; then
        fail_test "valid ipv4 cases" "isValidIP" "$addr"
    fi
    if ! isIPv4 "$addr"; then
            fail_test "valid ipv4 cases" "isIPv4" "$addr"
    fi
done

# Testing valid ipv6 cases
for addr in "${test_good_ipv6[@]}"; do
    if ! isValidIP "$addr"; then
        fail_test "valid ipv6 cases" "isValidIP" "$addr"
    fi
done

# Testing invalid ipv4 cases
for addr in "${test_bad_ipv4[@]}"; do
    if isValidIP "$addr"; then
        fail_test "invalid ipv4 cases" "isValidIP" "$addr"
    fi
    if isIPv4 "$addr"; then
        fail_test "invalid ipv4 cases" "isIPv4" "$addr"
    fi
done

# Testing invalid ipv6 cases
for addr in "${test_bad_ipv6[@]}"; do
    if isValidIP "$addr"; then
        fail_test "invalid ipv6 cases" "isValidIP" "$addr"
    fi
done

echo "Test has been completed successfully..."
