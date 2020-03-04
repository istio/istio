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

set -uf

function show_difference() {
    local ACTUAL=$1
    local EXPECTED=$2

    echo "${ACTUAL}"
    echo -e "\ndoesn't match expected result\n"
    echo "${EXPECTED}"
    diff -u <(echo "${EXPECTED}") <(echo "${ACTUAL}")  || true
}

function refresh_reference() {
    local TESTMODE=$1
    local NAME=$2
    local ACTUAL=$3

    echo "${ACTUAL}" > "${SCRIPT_DIR}/testdata/${TESTMODE}/${NAME}_golden.txt"
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
  local TEST_NAME="$1"
  local TEST_MODE="$2"
  local PARAMS="${3:-}"
  local ACTUAL_OUTPUT
  local FILE_UNDER_TEST

  case "${TEST_MODE}" in
   "golang")
    FILE_UNDER_TEST="${BINARY_PATH}/istio-iptables --dry-run --restore-format=false"
   ;;
   "golang_clean")
    FILE_UNDER_TEST="${BINARY_PATH}/istio-clean-iptables --dry-run"
   ;;
  esac

  if [[ ${TEST_NAME} == "clean" && ${TEST_MODE} == "golang" ]]; then
    PARAMS="--clean"
  fi

  # shellcheck disable=SC2086
  ACTUAL_OUTPUT="$(${FILE_UNDER_TEST} ${PARAMS} 2>/dev/null)"

  if [[ "x${REFRESH_GOLDEN:-false}x" = "xtruex" ]] ; then
    refresh_reference "${TEST_MODE}" "${TEST_NAME}" "${ACTUAL_OUTPUT}"
  else
    EXPECTED_OUTPUT=$(cat "${SCRIPT_DIR}/testdata/${TEST_MODE}/${TEST_NAME}_golden.txt")
    if assert_equals "${TEST_NAME}" "${ACTUAL_OUTPUT}" "${EXPECTED_OUTPUT}"; then
      echo -e "ok\tistio.io/$0/${TEST_NAME} (${TEST_MODE})\t0.000s"
    else
      echo "--- FAIL: ${TEST_NAME} (${TEST_MODE}) (0.00s)"
      echo "    ${SCRIPT_NAME}: ${TEST_NAME} output does not match with golden file"
      show_difference "${ACTUAL_OUTPUT}" "${EXPECTED_OUTPUT}"
      echo -e "FAIL\tistio.io/${SCRIPT_NAME}/${TEST_NAME}\t0.000s"
      FAILED+=("${TEST_NAME} (${TEST_MODE})")
    fi
  fi
}

TEST_MODES=( "$@" )

SCRIPT_NAME=$0
SCRIPT_DIR=$(dirname "$SCRIPT_NAME")
if [[ ${#TEST_MODES[@]} -eq 0 ]] ; then
    if [[ "x${REFRESH_GOLDEN:-false}x" != "xtruex" ]] ; then
        TEST_MODES+=("golang")
    fi
fi
export PATH="${SCRIPT_DIR}/stubs:${PATH}"

export BINARY_PATH="${LOCAL_OUT:-$ISTIO_OUT}"
FAILED=()

for TEST_MODE in "${TEST_MODES[@]}"; do

    compareWithGolden mode_redirect "${TEST_MODE}" "-p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2"
    compareWithGolden clean "${TEST_MODE}_clean"
    compareWithGolden mode_tproxy "${TEST_MODE}" "-p 12345 -u 4321 -g 4444 -m TPROXY -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2"
    compareWithGolden clean "${TEST_MODE}_clean"
    # compareWithGolden mode_tproxy_and_ipv6 "${TEST_MODE}" "-p 12345 -u 4321 -g 4444 -m TPROXY -b * -d 7777,8888 -i 2001:db8::/32 -x 2019:db8::/32 -k eth1,eth2"
    compareWithGolden clean "${TEST_MODE}_clean"
    compareWithGolden mode_tproxy_and_wildcard_port "${TEST_MODE}" "-p 12345 -u 4321 -g 4444 -m TPROXY -b * -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2"
    compareWithGolden clean "${TEST_MODE}_clean"
    compareWithGolden empty_parameter "${TEST_MODE}" ""
    compareWithGolden clean "${TEST_MODE}_clean"
    compareWithGolden outbound_port_exclude "${TEST_MODE}" "-p 12345 -u 4321 -g 4444 -o 1024,21 -m REDIRECT -b 5555,6666 -d 7777,8888 -i 1.1.0.0/16 -x 9.9.0.0/16 -k eth1,eth2"
    compareWithGolden clean "${TEST_MODE}_clean"
    compareWithGolden wildcard_include_ip_range "${TEST_MODE}" "-p 12345 -u 4321 -g 4444 -m REDIRECT -b 5555,6666 -d 7777,8888 -i * -x 9.9.0.0/16 -k eth1,eth2"
    compareWithGolden clean "${TEST_MODE}_clean"

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
