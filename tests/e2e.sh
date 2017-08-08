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

function error_exit() {
    # ${BASH_SOURCE[1]} is the file name of the caller.
    echo "${BASH_SOURCE[1]}: line ${BASH_LINENO[0]}: ${1:-Unknown Error.} (exit ${2:-1})" 1>&2
    exit ${2:-1}
}

. ${ROOT}/istio.VERSION || error_exit "Could not source versions"

TESTS_TARGETS=($(bazel query 'tests(//tests/e2e/tests/...)'))
FAILURE_COUNT=0
SUMMARY='Tests Summary'

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
    "./$bin_path" ${ARGS[@]} ${@} &> ${log_file} &
    pid=$!
    pid2testname["$pid"]=$bin_path
    pid2logfile["$pid"]=$log_file
done

echo "Running tests in parallel. Logs reported in serial order after all tests finish."

# Barrier until all forked processes finish
# also collects test results
for job in `jobs -p`; do
    wait $job
    if [ $? -eq 0 ]; then
        SUMMARY+="\nPASSED: ${pid2testname[$job]}"
    else
        let "FAILURE_COUNT+=1"
        SUMMARY+="\nFAILED: ${pid2testname[$job]}"
    fi
    echo '****************************************************'
    echo "Log from ${pid2testname[$job]}"
    echo '****************************************************'
    cat ${pid2logfile[$job]}
    echo
    rm -rf ${pid2logfile[$job]}
done

printf "${SUMMARY}\n"
exit ${FAILURE_COUNT}
