#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# Local vars
ROOT=$( cd "$( dirname "${BASH_SOURCE[0]}" )/.." && pwd )
ARGS=(-alsologtostderr -test.v -v 2)
TESTARGS=${@}

function print_block() {
    line=""
    for i in {1..50}
    do
        line+="$1"
    done

    echo $line
    echo $2
    echo $line
}

function error_exit() {
    # ${BASH_SOURCE[1]} is the file name of the caller.
    echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
    exit ${2:-1}
}

. ${ROOT}/istio.VERSION || error_exit "Could not source versions"
TESTS_TARGETS=($(bazel query 'tests(//tests/e2e/tests/...)'))
TOTAL_FAILURE=0
SUMMARY='Tests Summary'
RBAC_FILE='install/kubernetes/istio-rbac-beta.yaml'

function test_group() {
    print_block '=' "TEST Group"
    FAILURE_COUNT=0
    for T in ${TESTS_TARGETS[@]}; do
      print_block '.' "Running ${T}"
      bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_RUN_ARGS} ${T} -- ${ARGS[@]} ${TESTARGS[@]} --rbac_path ${RBAC_FILE}
      RET=${?}
      if [[ ${RET} -eq 0 ]]; then
        SUMMARY+="\nPASSED: ${T} "
      else
        SUMMARY+="\nFAILED: ${T} "
        ((FAILURE_COUNT++))
      fi
    done
    printf "${SUMMARY}\n\n\n"
    TOTAL_FAILURE+=${FAILURE_COUNT}
}

test_group

exit ${TOTAL_FAILURE}
