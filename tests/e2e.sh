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
TESTS_TARGETS=($(bazel query 'tests(//tests/e2e/tests/...)'))|| error_exit 'Could not find tests targets'
TOTAL_FAILURE=0
SUMMARY='Tests Summary'
RBAC_FILE='install/kubernetes/istio-rbac-beta.yaml'

PARALLEL_MODE=false

function process_result() {
    if [[ $1 -eq 0 ]]; then
        SUMMARY+="\nPASSED: $2 "
    else
        SUMMARY+="\nFAILED: $2 "
        ((FAILURE_COUNT++))
    fi
}

function concurrent_exec() {
    cd ${ROOT}
    declare -A pid2testname
    declare -A pid2logfile

    for T in ${TESTS_TARGETS[@]}; do
        # Compile test target
        bazel build ${T}
        # Construct path to binary using bazel target name
        BAZEL_RULE_PREFIX="//"
        BAZEL_BIN="bazel-bin/"
        bin_path=${T/$BAZEL_RULE_PREFIX/$BAZEL_BIN}
        bin_path=${bin_path/://}
        log_file="${bin_path///_}.log"
        # Run tests concurrently as subprocesses
        # Dup stdout and stderr to file
        "./$bin_path" ${ARGS[@]} ${TESTARGS[@]} --rbac_path ${RBAC_FILE} &> ${log_file} &
        pid=$!
        pid2testname["$pid"]=$bin_path
        pid2logfile["$pid"]=$log_file
    done

    echo "Running tests in parallel. Logs reported in serial order after all tests finish."

    # Barrier until all forked processes finish
    # also collects test results
    for job in `jobs -p`; do
        wait $job
        process_result $? ${pid2testname[$job]}
        echo '****************************************************'
        echo "Log from ${pid2testname[$job]}"
        echo '****************************************************'
        cat ${pid2logfile[$job]}
        echo
        rm -rf ${pid2logfile[$job]}
    done
}

function sequential_exec() {
    for T in ${TESTS_TARGETS[@]}; do
        echo '****************************************************'
        echo "Running ${T}"
        echo '****************************************************'
        bazel ${BAZEL_STARTUP_ARGS} run ${BAZEL_RUN_ARGS} ${T} -- ${ARGS[@]} ${TESTARGS[@]} --rbac_path ${RBAC_FILE}
        process_result $? ${T}
        echo '****************************************************'
    done
}

# getopts only handles single character flags
for ((i=1; i<=$#; i++)); do
    case ${!i} in
        -p|--parallel) PARALLEL_MODE=true
        continue
        ;;
    esac
    # Filter -p out as it is not defined in the test framework
    ARGS+=( ${!i} )
done

if $PARALLEL_MODE ; then
    echo "Executing tests in parallel"
    concurrent_exec
else
    echo "Executing tests sequentially"
    sequential_exec
fi

printf "${SUMMARY}\n"
exit ${FAILURE_COUNT}
