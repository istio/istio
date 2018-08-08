#!/bin/bash
set -e
set -u

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname ${SCRIPTPATH})"
DIR="./..."
CODECOV_SKIP="${ROOTDIR}/codecov.skip"
SKIPPED_TESTS_GREP_ARGS=

if [ "${1:-}" != "" ]; then
    DIR="./$1/..."
fi

COVERAGEDIR="$(mktemp -d /tmp/XXXXX.coverage)"
mkdir -p $COVERAGEDIR

# coverage test needs to run one package per command.
# This script runs nproc/2 in parallel.
# Script fails if any one of the tests fail.

# half the number of cpus seem to saturate
if [[ -z ${MAXPROCS:-} ]];then
  MAXPROCS=$(($(getconf _NPROCESSORS_ONLN)/2))
fi
PIDS=()
FAILED_TESTS=()

declare -a PKGS

function code_coverage() {
  local filename
  filename="$(echo ${1} | tr '/' '-')"
  ( go test \
    -coverprofile=${COVERAGEDIR}/${filename}.cov \
    -covermode=atomic ${1} \
    | tee ${COVERAGEDIR}/${filename}.report ) &
  local pid=$!
  PKGS[${pid}]=${1}
  PIDS+=(${pid})
}

function wait_for_proc() {
  local num
  num=$(jobs -p | wc -l)
  while [ ${num} -gt ${MAXPROCS} ]; do
    sleep 2
    num=$(jobs -p|wc -l)
  done
}

function join_procs() {
  local p
  for p in ${PIDS[@]}; do
      if ! wait ${p}; then
          FAILED_TESTS+=(${PKGS[${p}]})
      fi
  done
}

function parse_skipped_tests() {
  while read -r entry; do
    if [[ "${SKIPPED_TESTS_GREP_ARGS}" != '' ]]; then
      SKIPPED_TESTS_GREP_ARGS+='\|'
    fi
    SKIPPED_TESTS_GREP_ARGS+="\\(${entry}\\)"
  done < "${CODECOV_SKIP}"
}

cd "${ROOTDIR}"

parse_skipped_tests

echo "Code coverage test (concurrency ${MAXPROCS})"
for P in $(go list ${DIR} | grep -v vendor); do
  if echo ${P} | grep -q "${SKIPPED_TESTS_GREP_ARGS}"; then
    echo "Skipped ${P}"
    continue
  fi
  code_coverage "${P}"
  wait_for_proc
done

join_procs

touch "${COVERAGEDIR}/empty"
FINAL_CODECOV_DIR="${GOPATH}/out/codecov"
mkdir -p "${FINAL_CODECOV_DIR}"
pushd "${FINAL_CODECOV_DIR}"
go get github.com/wadey/gocovmerge
gocovmerge "${COVERAGEDIR}"/*.cov > coverage.cov
cat "${COVERAGEDIR}"/*.report > codecov.report
popd
echo "Reports are stored in ${FINAL_CODECOV_DIR}"


if [[ -n ${FAILED_TESTS:-} ]]; then
  echo "The following tests failed"
  for T in ${FAILED_TESTS[@]}; do
    echo "FAIL: $T"
  done
  exit 1
fi

PKG_CHECK_ARGS=(--bucket='' )
if [[ -n "${CIRCLE_BUILD_NUM:-}" ]]; then
  if [[ -z "${CIRCLE_PR_NUMBER:-}" ]]; then
    TMP_SA_JSON=$(mktemp /tmp/XXXXX.json)
    ENCRYPTED_SA_JSON="${ROOTDIR}/.circleci/accounts/istio-circle-ci.gcp.serviceaccount"
    openssl aes-256-cbc -d -in "${ENCRYPTED_SA_JSON}" -out "${TMP_SA_JSON}" -k "${GCS_BUCKET_TOKEN}" -md sha256
    # only pushing data on post submit
    PKG_CHECK_ARGS=( --build_id="${CIRCLE_BUILD_NUM}"
      --job_name="istio/${CIRCLE_JOB}_${CIRCLE_BRANCH}"
      --service_account="${TMP_SA_JSON}"
    )
  fi
fi

echo 'Checking package coverage'
go get -u istio.io/test-infra/toolbox/pkg_check
pkg_check \
  --report_file=${FINAL_CODECOV_DIR}/codecov.report \
  --alsologtostderr \
  --requirement_file=codecov.requirement "${PKG_CHECK_ARGS[@]}"
