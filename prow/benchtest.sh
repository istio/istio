#!/bin/bash

# Copyright Istio Authors
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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_and_export_git_sha

set -eux

GCS_BENCHMARK_DIR="${GCS_BENCHMARK_DIR:-istio-prow/benchmarks}"

BENCHMARK_COUNT="${BENCHMARK_COUNT:-5}"
BENCHMARK_CPUS="${BENCHMARK_CPUS:-8}"

REPORT_JUNIT="${REPORT_JUNIT:-${ARTIFACTS}/junit_benchmarks.xml}"
REPORT_PLAINTEXT="${REPORT_PLAINTEXT:-${ARTIFACTS}/benchmark-log.txt}"

# Sha we should compare against. Defaults to the PULL_BASE_SHA, which is the last commit on the branch we are on.
# For example, a PR on master will compare to the HEAD of master.
COMPARE_GIT_SHA="${COMPARE_GIT_SHA:-${PULL_BASE_SHA:-${GIT_SHA}}}"

case "${1}" in
  run)
    shift
    benchmarkjunit "$@" -l "${REPORT_PLAINTEXT}" --output="${REPORT_JUNIT}" \
      --test-arg "--benchmem" \
      --test-arg "--count=${BENCHMARK_COUNT}" \
      --test-arg "--cpu=${BENCHMARK_CPUS}" \
      --test-arg "--test.timeout=30m"
    # Print out the results as well for ease of debugging, so they are in the logs instead of just output
    cat "${REPORT_PLAINTEXT}"
    ;;
  report)
    # Upload the reports to a well known path based on git SHA
    gsutil cp "${REPORT_JUNIT}" "gs://${GCS_BENCHMARK_DIR}/${GIT_SHA}.xml"
    gsutil cp "${REPORT_PLAINTEXT}" "gs://${GCS_BENCHMARK_DIR}/${GIT_SHA}.txt"
    ;;
  compare)
    # Fetch previous results, and compare them.
    curl "https://storage.googleapis.com/${GCS_BENCHMARK_DIR}/${COMPARE_GIT_SHA}.txt" > "${ARTIFACTS}/baseline-benchmark-log.txt"
    benchstat "${ARTIFACTS}/baseline-benchmark-log.txt" "${REPORT_PLAINTEXT}"
    ;;
  *)
    echo "unknown command, expect report, run, or compare."
    ;;
esac
