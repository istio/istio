#!/bin/bash

#
# Shell script for checking go benchmarks against baseline files.
#
# This shell script searches for "bench.baseline" files in the project folder, runs benchmarks in those folders
# and compares its results to the baselines defined in the file.
#
# To activate baseline for a benchmark, simply create a bench.baseline file in the same folder that the
# benchmark resides.
#
# The format of the file is pretty simple: Simply copy paste the result lines from the output of
#   `go test -bench=. -benchmem`
#
# Example:
#  --- BEGIN FILE ---
#  BenchmarkInterpreter/true_&&_false-8            	10000000	       120 ns/op	       0 B/op	       0 allocs/op
#  BenchmarkInterpreter/true_&&_true-8             	10000000	       118 ns/op	       0 B/op	       0 allocs/op
#  BenchmarkInterpreter/false_&&_false-8           	20000000	       117 ns/op	       0 B/op	       0 allocs/op
#  --- END FILE ---
#
# You can also add comments to the file:
#  --- BEGIN FILE ---
#  # This is a comment line.
#  BenchmarkInterpreter/true_&&_false-8            	10000000	       120 ns/op	       0 B/op	       0 allocs/op
#  BenchmarkInterpreter/true_&&_true-8             	10000000	       118 ns/op	       0 B/op	       0 allocs/op
#  BenchmarkInterpreter/false_&&_false-8           	20000000	       117 ns/op	       0 B/op	       0 allocs/op
#  --- END FILE ---
#
# If you want to skip comparison of a particular metric, simply put "-" in place of that metric value.
#  --- BEGIN FILE ---
#  # This is a comment line.
#  BenchmarkInterpreter/true_&&_false-8            	10000000	       - ns/op	           0 B/op	       0 allocs/op
#  BenchmarkInterpreter/true_&&_true-8             	10000000	       118 ns/op	       - B/op	       0 allocs/op
#  BenchmarkInterpreter/false_&&_false-8           	20000000	       117 ns/op	       0 B/op	       - allocs/op
#  --- END FILE ---
#
# Benchmarks in the results with no matching entries in the baseline file will be ignored. If the baseline
# file refers to a missing benchmark, then it is an error.
#
# Each value is compared within a tolerance range. The percentage values are defined in this file.
# The percentage ranges work both ways (i.e. both lower and higher values that fall outside the tolerance
# range is considered an error). This is intentional, as we'd like to encourage frequent updating of baselines
# to lock in any perf gains.


# The name of the baseline files that should be searched for.
BASELINE_FILENAME="bench.baseline"

# Percent tolerance for run time per op
# 50% is an unreasonable tolerance, but a good one start with, especially to not cause too many false positives initially.
# Once we establish a time-dilation calculation model, we can tighten the tolerance.
TOLERANCE_PERCENT_TIME_PER_OP=50

# Percent tolerance for allocated bytes per op
# There is generally some variance in this number, presumably because of upfront costs or timings.
TOLERANCE_PERCENT_BYTES_PER_OP=1

# Percent tolerance for allocations per op
# Start with 0, as this is one of the most stable numbers in the benchmarks.
TOLERANCE_PERCENT_ALLOCS_PER_OP=0

# the location of this script
SCRIPTPATH=$( cd "$(dirname "$0")" && pwd -P )

# the root folder for the project
ROOT=${SCRIPTPATH}/..
TARGET_DIR=${ROOT}
if ! [ -z "$1" ]; then
    TARGET_DIR=$1
fi

# Search and find the baseline files within the project
function findBaselineFiles() {
    find ${TARGET_DIR} -name ${BASELINE_FILENAME}
}

# load the baseline file with the name as first parameter
# filters comments/empty lines and cleans up the file.
function loadBaselineFile() {
    local FILENAME="${1}"
    local CONTENTS=$(cat "${FILENAME}")

    # Filter comment lines
    # shellcheck disable=SC2001
    local CONTENTS=$(echo "${CONTENTS}" | sed -e 's/#.*$//')

    # Filter empty lines
    # shellcheck disable=SC2001
    local CONTENTS=$(echo "${CONTENTS}" | sed -e '/^$/d')

    printf "%s" "${CONTENTS}"
}

# given an entry in either the baseline or the result output, returns the value of the indicated column
function getColumn() {
    local ENTRY="${1}"
    local COLUMN="${2}"
    local PARTS=( ${ENTRY} )
    echo ${PARTS[${COLUMN}]}
}

# given the cleaned-up contents of the baseline or the result output, find and return the entry matching the named benchmark.
function findEntry() {
    local DATA="${1}"
    local NAME="${2}"

    # Remove the CPU count suffix.
    local NAME="${NAME%-*}"

    printf "%s" "${DATA}" | grep "${NAME}"
}

# compare the given metric and return "-1", "0" or "1".
# the parameters, in order, are 1) the baseline value, 2) the result value, 3) the tolarance as a percent.
# comparison returns "-1" or "1", if the result is out of bounds of the tolerance limit. returns "0" if
# the result is within tolerance, or if the baseline value indicates that it shouldn't be compared with "-".
function compareMetric() {
    local BASELINE="${1}"
    local RESULT="${2}"
    local TOLERANCE_PERCENT="${3}"

    if [ "${BASELINE}" == "-" ]; then
        # Skip this comparison
        echo "0"
    else
        # calculate tolerance as absolute value, and perform comparison.
        local TOLERANCE=$(echo - | awk "{print ${BASELINE} * ${TOLERANCE_PERCENT} / 100}")

        local IS_HIGH=$(echo - | awk "{print (${RESULT} > (${BASELINE} + ${TOLERANCE}))}")
        local IS_LOW=$(echo - | awk "{print (${RESULT} < (${BASELINE} - ${TOLERANCE}))}")

        if [ "${IS_HIGH}" == 1 ]; then
            echo "1"
        elif [ "${IS_LOW}" == 1 ]; then
            echo "-1"
        else
            echo "0"
        fi
    fi
}

# Given the contents of a baseline, and the results of a test, compare and report.
function compareBenchResults() {
    local BASELINE="${1}"
    local RESULTS="${2}"

    printf '%s' "${BASELINE}" | while read -r BASELINE_ENTRY; do
        local BENCH_NAME=$(getColumn "${BASELINE_ENTRY}" "0")
        local RESULT_ENTRY=$(findEntry "${RESULTS}", "${BENCH_NAME}")

        if [ -z "${RESULT_ENTRY// }" ]; then
            echo "FAILED ${BENCH_NAME}"
            echo "Baseline:"
            echo "   ${BASELINE_ENTRY}"
            echo "Details:"
            echo "   No matching benchmark encountered."
        else
            local BASELINE_TIME_PER_OP=$(getColumn "${BASELINE_ENTRY}" "2")
            local BASELINE_BYTES_PER_OP=$(getColumn "${BASELINE_ENTRY}" "4")
            local BASELINE_ALLOCS_PER_OP=$(getColumn "${BASELINE_ENTRY}" "6")

            local RESULT_TIME_PER_OP=$(getColumn "${RESULT_ENTRY}" "2")
            local RESULT_BYTES_PER_OP=$(getColumn "${RESULT_ENTRY}" "4")
            local RESULT_ALLOCS_PER_OP=$(getColumn "${RESULT_ENTRY}" "6")

            local TIME_PER_OP_CMP=$(compareMetric "${BASELINE_TIME_PER_OP}" "${RESULT_TIME_PER_OP}" "${TOLERANCE_PERCENT_TIME_PER_OP}")
            local BYTES_PER_OP_CMP=$(compareMetric "${BASELINE_BYTES_PER_OP}" "${RESULT_BYTES_PER_OP}" "${TOLERANCE_PERCENT_BYTES_PER_OP}")
            local ALLOCS_PER_OP_CMP=$(compareMetric "${BASELINE_ALLOCS_PER_OP}" "${RESULT_ALLOCS_PER_OP}" "${TOLERANCE_PERCENT_ALLOCS_PER_OP}")

            if [ "${TIME_PER_OP_CMP}" != "0" ] || [ "${BYTES_PER_OP_CMP}" != "0" ] || [ "${ALLOCS_PER_OP_CMP}" != "0" ]; then
                echo "--FAILED ${BENCH_NAME}"
                echo "Baseline:"
                echo "   ${BASELINE_ENTRY}"
                echo "Result:"
                echo "   ${RESULT_ENTRY}"
                echo "Details:"

                if [ "${TIME_PER_OP_CMP}" != "0" ]; then
                    echo -e "  ${RESULT_TIME_PER_OP} ns/op is not in range of: ${BASELINE_TIME_PER_OP}   \\t[tol: ${TOLERANCE_PERCENT_TIME_PER_OP}%]"
                fi

                if [ "${BYTES_PER_OP_CMP}" != "0" ]; then
                    echo -e "  ${RESULT_BYTES_PER_OP} B/s is not in range of: ${BASELINE_BYTES_PER_OP}        \\t[tol: ${TOLERANCE_PERCENT_BYTES_PER_OP}%]"
                fi

                if [ "${ALLOCS_PER_OP_CMP}" != "0" ]; then
                    echo -e "  ${RESULT_ALLOCS_PER_OP} allocs/op is not in range of: ${BASELINE_ALLOCS_PER_OP}   \\t[tol: ${TOLERANCE_PERCENT_ALLOCS_PER_OP}%]"
                fi
                printf '\n\n'
            fi
        fi
    done
}

# sanitizes the scraped output obtained from "go test"
function cleanupBenchResult() {
    local OUTPUT=${1}
    # Remove known extraneous lines
    local OUTPUT=$(printf '%s' "${OUTPUT}" | sed -e 's/^goos:.*$//')
    local OUTPUT=$(printf '%s' "${OUTPUT}" | sed -e 's/^goarch:.*$//')
    local OUTPUT=$(printf '%s' "${OUTPUT}" | sed -e 's/^pkg:.*$//')
    local OUTPUT=$(printf '%s' "${OUTPUT}" | sed -e 's/^PASS.*$//')
    local OUTPUT=$(printf '%s' "${OUTPUT}" | sed -e 's/^ok.*$//')
    printf "%s" "${OUTPUT}" | sed -e "/^$/d"
}

# main entry point.
function run() {
    local ERR="0"

    BASELINE_FILES=$(findBaselineFiles)
    echo "Found the following benchmark baseline files:"
    for BASELINE_FILENAME in ${BASELINE_FILES}; do
        echo "  ${BASELINE_FILENAME}"
    done
    echo
    echo

    for BASELINE_FILENAME in ${BASELINE_FILES}; do
        local BENCH_DIR=$(dirname "${BASELINE_FILENAME}")

        echo "Running benchmarks in dir: ${BENCH_DIR}"

        local BASELINE=$(loadBaselineFile "${BASELINE_FILENAME}")

        pushd "${BENCH_DIR}"  > /dev/null 2>&1 || return

        local BENCH_RESULT=$(go test -bench=. -benchmem -run=^$)
        local BENCH_RESULT=$(cleanupBenchResult "${BENCH_RESULT}")

        printf 'Current benchmark results:\n'
        printf '%s' "${BENCH_RESULT}"
        printf '\n\n'

        local CMP_RESULT=$(compareBenchResults "${BASELINE}" "${BENCH_RESULT}")

        if [[ -n "${CMP_RESULT// }" ]]; then
            printf "%s" "${CMP_RESULT}"
            ERR="-1"
        fi
        popd > /dev/null 2>&1 || return

    done

    exit "${ERR}"
}


run


# The code below this line is for testing purposes.

function test_compareMetric() {
    local ZERO_CASES[0]=$(compareMetric "0" "0" "0")
    local ZERO_CASES[1]=$(compareMetric "100" "100" "0")
    local ZERO_CASES[2]=$(compareMetric "100" "110" "10")
    local ZERO_CASES[3]=$(compareMetric "100" "91" "10")
    local ZERO_CASES[4]=$(compareMetric "-" "55" "0")

    for i in "${ZERO_CASES[@]}"; do
        if [ "${i}" != "0" ]; then
            echo "Zero case failure: ${ZERO_CASES[*]}"
            exit -1
        fi
    done

    local ONE_CASES[0]=$(compareMetric "0" "1" "0")
    local ONE_CASES[1]=$(compareMetric "100" "111" "10")

    for i in "${ONE_CASES[@]}"; do
        if [ "${i}" != "1" ]; then
            echo "One case failure: ${ONE_CASES[*]}"
            exit -1
        fi
    done

    local MINUS_ONE_CASES[0]=$(compareMetric "0" "-1" "0")
    local MINUS_ONE_CASES[1]=$(compareMetric "100" "89" "10")

    for i in "${MINUS_ONE_CASES[@]}"; do
        if [ "${i}" != "-1" ]; then
            echo "Minus one case failure: ${MINUS_ONE_CASES[*]}"
            exit -1
        fi
    done
}

function test_compareBenchResult() {
    local SUCCESS_CASES[0]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
")

    local SUCCESS_CASES[1]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       - ns/op	           0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
")

    local SUCCESS_CASES[2]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       140 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
")

    local SUCCESS_CASES[3]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       140 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       0 allocs/op
")

    local SUCCESS_CASES[4]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       - B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       25 B/op	       0 allocs/op
")

    local SUCCESS_CASES[5]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       - allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
")

    local SUCCESS_CASES[6]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       - allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
BenchmarkInterpreter/ExprBench/ExtraBench-8        	10000000	       136 ns/op	       0 B/op	       0 allocs/op
")

    local SUCCESS_CASES[7]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8            10000000           136 ns/op           0 B/op          0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-32           10000000           136 ns/op           0 B/op          0 allocs/op
")


    local SUCCESS_CASES[7]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-32         	10000000	       136 ns/op	       0 B/op	       0 allocs/op
")


    for i in "${SUCCESS_CASES[@]}"; do
        if [[ -n "${i// }" ]]; then
            echo "Success case failure: ${SUCCESS_CASES[*]}"
            exit -1
        fi
    done

        local FAILURE_CASES[0]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       100 ns/op	       0 B/op	       0 allocs/op
")

        local FAILURE_CASES[1]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       181 ns/op	       0 B/op	       0 allocs/op
")

        local FAILURE_CASES[2]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       1 B/op	       0 allocs/op
")

        local FAILURE_CASES[3]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       1 B/op	       0 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       0 allocs/op
")

        local FAILURE_CASES[4]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       1 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       10 allocs/op
")

        local FAILURE_CASES[5]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       10 allocs/op
" \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       1 allocs/op
")

        local FAILURE_CASES[5]=$(compareBenchResults \
"BenchmarkInterpreter/ExprBench/ok_1st-8         	10000000	       150 ns/op	       0 B/op	       10 allocs/op
" \
"BenchmarkInterpreter/ExprBench/some-other-8       	10000000	       150 ns/op	       0 B/op	       10 allocs/op
")

    for i in "${FAILURE_CASES[@]}"; do
        if [[ -z "${i// }" ]]; then
            echo "Failure case failures"
            exit -1
        fi
    done
}

test_compareMetric
test_compareBenchResult
