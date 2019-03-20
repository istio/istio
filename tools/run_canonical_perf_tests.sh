#!/bin/bash

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
# shellcheck source=tools/setup_perf_cluster.sh
source "${DIR}/setup_perf_cluster.sh"

LABEL="${1}"
OUT_DIR="${2}"

if [[ -z "${OUT_DIR// }" ]]; then
  OUT_DIR=$(mktemp -d -t "istio_perf.XXXXXX")
fi

DURATION="1m"

run_canonical_perf_test "${LABEL}" "fortio2" "echo1"  100  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1"  400  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1000  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1200  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1600  "${DURATION}" 16 "${OUT_DIR}"

run_canonical_perf_test "${LABEL}" "fortio1" "echo2"  100  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2"  400  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1000  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1200  "${DURATION}" 16 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1600  "${DURATION}" 16 "${OUT_DIR}"

run_canonical_perf_test "${LABEL}" "fortio2" "echo1"  100  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1"  400  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1000  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1200  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1600  "${DURATION}" 20 "${OUT_DIR}"

run_canonical_perf_test "${LABEL}" "fortio1" "echo2"  100  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2"  400  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1000  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1200  "${DURATION}" 20 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1600  "${DURATION}" 20 "${OUT_DIR}"

run_canonical_perf_test "${LABEL}" "fortio2" "echo1"  100  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1"  400  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1000  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1200  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio2" "echo1" 1600  "${DURATION}" 24 "${OUT_DIR}"

run_canonical_perf_test "${LABEL}" "fortio1" "echo2"  100  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2"  400  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1000  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1200  "${DURATION}" 24 "${OUT_DIR}"
run_canonical_perf_test "${LABEL}" "fortio1" "echo2" 1600  "${DURATION}" 24 "${OUT_DIR}"

python "${DIR}/convert_perf_results.py" "${OUT_DIR}" > "${OUT_DIR}/out.csv"
