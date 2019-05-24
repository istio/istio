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

set -eu

function show_difference() {
    local ACTUAL=$1
    local EXPECTED=$2

    echo "${ACTUAL}"
    echo -e "\ndoesn't match expected result\n"
    echo "${EXPECTED}"
    diff -u <(echo "${ACTUAL}") <(echo "${EXPECTED}") || true
}

function refresh_reference() {
    local NAME=$1
    local ACTUAL=$2

    echo "${ACTUAL}" > "tests/scripts/testdata/${NAME}_golden.txt"
    echo "golden file for test ${NAME} updated"
}

function assert_equals() {
    local NAME=$1
    local ACTUAL=$2
    local EXPECTED=$3

    if [ "${ACTUAL}" != "${EXPECTED}" ]; then
        return 1
    else
        return 0
    fi
}

FILE_UNDER_TEST=./tools/packaging/common/istio-iptables.sh

export PATH="${PWD}/tests/scripts/stubs:${PATH}"

SCRIPT_NAME=$0
declare -A TESTS
FAILED=()
TESTS[mode_redirect]="-p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888 -i 1.1.1.0/16 -x 9.9.9.0/16 -k eth1,eth2"
TESTS[mode_tproxy]="-p 12345 -u 4321 -g 4444 -m TPROXY -b 5555,6666 -d 7777,8888 -i 1.1.1.0/16 -x 9.9.9.0/16 -k eth1,eth2"
TESTS[mode_tproxy_and_ipv6]="-p 12345 -u 4321 -g 4444 -m TPROXY -b * -d 7777,8888 -i 2001:db8:1::1/32 -x 2019:db8:1::1/32 -k eth1,eth2"
TESTS[mode_tproxy_and_wildcard_port]="-p 12345 -u 4321 -g 4444 -m TPROXY -b * -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2"
TESTS[empty_parameter]=""
TESTS[outbound_port_exclude]="-p 12345 -u 4321 -g 4444 -o 1024,21 -m REDIRECT -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2"
TESTS[wildcard_include_ip_range]="-p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888 -i * -x 9.9.0.0/16 -k eth1,eth2"
TESTS[clean]="Clean"

for TEST_NAME in "${!TESTS[@]}"
do
  TEST_ARGS=${TESTS[$TEST_NAME]}

  # shellcheck disable=SC2086
  ACTUAL_OUTPUT=$(${FILE_UNDER_TEST} ${TEST_ARGS}  2>/dev/null)
  
  if [[ "x${REFRESH_GOLDEN:-false}x" = "xtruex" ]] ; then
    refresh_reference "${TEST_NAME}" "${ACTUAL_OUTPUT}"
  else
    EXPECTED_OUTPUT=$(cat "tests/scripts/testdata/${TEST_NAME}_golden.txt")
    if assert_equals "${TEST_NAME}" "${ACTUAL_OUTPUT}" "${EXPECTED_OUTPUT}"; then
      echo -e "ok\tistio.io/$0/${TEST_NAME}\t0.000s"
    else
      echo "--- FAIL: ${TEST_NAME} (0.00s)"
      echo "    ${SCRIPT_NAME}: ${TEST_NAME} output does not match with golden file"
      show_difference "${ACTUAL_OUTPUT}" "${EXPECTED_OUTPUT}"
      echo -e "FAIL\tistio.io/${SCRIPT_NAME}/${TEST_NAME}\t0.000s"
      FAILED+=("${TEST_NAME}")
    fi
  fi
done

NUMBER_FAILING=${#FAILED[@]}
if [[ ${NUMBER_FAILING} -eq 0 ]] ; then
    echo -e "\nAll tests were successful"
else
    echo -e "\n${NUMBER_FAILING} test(s) failed:"
    for FAILING_TEST in "${FAILED[@]}"
    do
        echo "  - ${FAILING_TEST}"
    done
fi

exit "${NUMBER_FAILING}"