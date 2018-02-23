#!/usr/bin/env bash
set -e
set -u

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname ${SCRIPTPATH})"
DIR="./..."

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
  MAXPROCS=$[$(getconf _NPROCESSORS_ONLN)/2]
fi
PIDS=()
FAILED_TESTS=()

declare -a PKGS

function code_coverage() {
  local filename="$(echo ${1} | tr '/' '-')"
    go test -coverprofile=${COVERAGEDIR}/${filename}.txt ${1} &
    local pid=$!
    PKGS[${pid}]=${1}
    PIDS+=(${pid})
}

function wait_for_proc() {
    local num=$(jobs -p | wc -l)
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

cd "${ROOTDIR}"

echo "Code coverage test (concurrency ${MAXPROCS})"
for P in $(go list ${DIR} | grep -v vendor); do
    #FIXME remove mixer tools exclusion after tests can be run without bazel
    if [[ ${P} == "istio.io/istio/tests"* || \
      ${P} == "istio.io/istio/mixer/tools/codegen"* ]];then
      echo "Skipped ${P}"
      continue
    fi
    code_coverage "${P}"
    wait_for_proc
done

join_procs

touch "${COVERAGEDIR}/empty"
cat "${COVERAGEDIR}"/* > coverage.txt

if [[ -n ${FAILED_TESTS} ]]; then
  echo "The following tests failed"
  for T in ${FAILED_TESTS[@]}; do
    echo "FAIL: $T"
  done
  exit 1
fi
