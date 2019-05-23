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
        diff -u <(echo "$1") <(echo "$2")
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
EXPECTED_OUTPUT=$(cat tests/scripts/testdata/mode_redirect_golden.txt)
assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"


# Test mode TPROXY
OUTPUT=$($TEST_SHELL $TEST_FILE -p 12345 -u 4321 -g 4444 -m TPROXY -b 5555,6666 -d 7777,8888  -i 1.1.1.0/16 -x 9.9.9.0/16  -k eth1,eth2 2>/dev/null)
EXPECTED_OUTPUT=$(cat tests/scripts/testdata/mode_tproxy_golden.txt)
assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"

# Test empty parameter
OUTPUT=$($TEST_SHELL $TEST_FILE 2>/dev/null)
EXPECTED_OUTPUT=$(cat tests/scripts/testdata/empty_parameter_golden.txt)
assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"

# Test outbound port exclusion
OUTPUT=$($TEST_SHELL $TEST_FILE -p 12345 -u 4321 -g 4444 -o 1024,21 -m REDIRECT -b 5555,6666 -d 7777,8888  -i 1.1.0.0/16 -x 9.9.0.0/16  -k eth1,eth2  2>/dev/null)
EXPECTED_OUTPUT=$(cat tests/scripts/testdata/outbound_port_exclude_golden.txt)
assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"

echo "Test was successful"