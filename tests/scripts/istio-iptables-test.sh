#!/usr/bin/env bash
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

set -e

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

FILE_UNDER_TEST=./tools/packaging/common/istio-iptables.sh

export PATH="${PWD}/tests/scripts/stubs:${PATH}"

declare -A TESTS
TESTS[mode_redirect]="-p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888  -i 1.1.1.0/16 -x 9.9.9.0/16  -k eth1,eth2"
TESTS[mode_tproxy]="-p 12345 -u 4321 -g 4444 -m TPROXY -b 5555,6666 -d 7777,8888  -i 1.1.1.0/16 -x 9.9.9.0/16  -k eth1,eth2"
TESTS[empty_parameter]=""
TESTS[outbound_port_exclude]="-p 12345 -u 4321 -g 4444 -o 1024,21 -m REDIRECT -b 5555,6666 -d 7777,8888  -i 1.1.0.0/16 -x 9.9.0.0/16  -k eth1,eth2"

for TEST_NAME in "${!TESTS[@]}"
do
  echo "running test $TEST_NAME"
  TEST_ARGS=${TESTS[$TEST_NAME]}

  # shellcheck disable=SC2086
  OUTPUT=$($FILE_UNDER_TEST $TEST_ARGS  2>/dev/null)
  EXPECTED_OUTPUT=$(cat "tests/scripts/testdata/${TEST_NAME}_golden.txt")
  assert_equals "$OUTPUT" "$EXPECTED_OUTPUT"
done

echo "Tests were successful"