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
    diff -u <(echo "${EXPECTED}") <(echo "${ACTUAL}")  || true
}

function refresh_reference() {
    local NAME=$1
    local ACTUAL=$2

    echo "${ACTUAL}" > "${SCRIPT_DIR}/testdata/${NAME}_golden.txt"
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

function compareWithGolden() {
  TEST_NAME="$1"
  ACTUAL_OUTPUT="$2"

  if [[ "x${REFRESH_GOLDEN:-false}x" = "xtruex" ]] ; then
    refresh_reference "${TEST_NAME}" "${ACTUAL_OUTPUT}"
  else
    EXPECTED_OUTPUT=$(cat "${SCRIPT_DIR}/testdata/${TEST_NAME}_golden.txt")
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
}

SCRIPT_NAME=$0
SCRIPT_DIR=$(dirname "$SCRIPT_NAME")
#FILE_UNDER_TEST="${SCRIPT_DIR}/../../tools/packaging/common/istio-iptables.sh"
FILE_UNDER_TEST="${SCRIPT_DIR}/../../tools/istio-iptables/istio-iptables --dryRun"
export PATH="${SCRIPT_DIR}/stubs:${PATH}"

FAILED=()

compareWithGolden mode_redirect "$(${FILE_UNDER_TEST} -p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2 2>/dev/null)"
compareWithGolden mode_tproxy "$(${FILE_UNDER_TEST} -p 12345 -u 4321 -g 4444 -m TPROXY -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2 2>/dev/null)"
compareWithGolden mode_tproxy_and_ipv6 "$(${FILE_UNDER_TEST} -p 12345 -u 4321 -g 4444 -m TPROXY -b "*" -d 7777,8888 -i 2001:db8:1::1/32 -x 2019:db8:1::1/32 -k eth1,eth2 2>/dev/null)"
compareWithGolden mode_tproxy_and_wildcard_port "$(${FILE_UNDER_TEST} -p 12345 -u 4321 -g 4444 -m TPROXY -b "*" -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2 2>/dev/null)"
compareWithGolden empty_parameter "$(${FILE_UNDER_TEST} 2>/dev/null)"
compareWithGolden outbound_port_exclude "$(${FILE_UNDER_TEST} -p 12345 -u 4321 -g 4444 -o 1024,21 -m REDIRECT -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2 2>/dev/null)"
compareWithGolden wildcard_include_ip_range "$(${FILE_UNDER_TEST} -p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888 -i "*" -x 9.9.0.0/16 -k eth1,eth2 2>/dev/null)"
compareWithGolden clean "$(${FILE_UNDER_TEST} Clean 2>/dev/null)"

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