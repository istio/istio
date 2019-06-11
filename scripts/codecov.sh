#!/bin/bash

# Copyright 2019 Istio Authors
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

set -e
set -u
set -o pipefail

# Output directory where reports are written. This can be specified outside.
OUT_DIR=${OUT_DIR:-"${GOPATH}/out/codecov"}

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
echo $SCRIPTPATH
ROOTDIR="$(dirname "${SCRIPTPATH}")"
echo $ROOTDIR
DIR="./..."
CODECOV_SKIP=${CODECOV_SKIP:-"${ROOTDIR}/codecov.skip"}
SKIPPED_TESTS_GREP_ARGS=
TEST_RETRY_COUNT=3

# Set GOPATH to match the expected layout
GO_TOP=$(cd "$(dirname "$0")"/../../../..; pwd)

export GOPATH=${GOPATH:-$GO_TOP}

if [[ "${1:-}" != "" ]]; then
    DIR="./$1/..."
fi

COVERAGEDIR="$(mktemp -d /tmp/istio_operator_coverage.XXXXXXXXXX)"
mkdir -p "$COVERAGEDIR"

# TODO:Setup environment needed by some tests.

# coverage test needs to run one package per command.
# This script runs nproc/2 in parallel.
# Script fails if any one of the tests fail.

# half the number of cpus seem to saturate
if [[ -z ${MAXPROCS:-} ]];then
  MAXPROCS=$(($(getconf _NPROCESSORS_ONLN)/2))
fi

function parse_skipped_tests() {
  while read -r entry; do
    if [[ "${SKIPPED_TESTS_GREP_ARGS}" != '' ]]; then
      SKIPPED_TESTS_GREP_ARGS+='\|'
    fi
    if [[ "${entry}" != "#"* ]]; then
      SKIPPED_TESTS_GREP_ARGS+="\\(${entry}\\)"
    fi
  done < "${CODECOV_SKIP}"
}

parse_skipped_tests

function remove_skipped_tests_from_cov() {
  while read -r entry; do
    entry="$(echo "${entry}" | sed 's/\//\\\//g')"
    sed -i "/${entry}/d" "$1"
  done < "${CODECOV_SKIP}"
}

function code_coverage() {
  local filename
  local count=${2:-0}
  filename="$(echo "${1}" | tr '/' '-')"
  echo ${filename}
  go test \
    -coverpkg=istio.io/operator/... \
    -coverprofile="${COVERAGEDIR}/${filename}.cov" \
    -covermode=atomic "${1}" \
    | tee "${COVERAGEDIR}/${filename}.report" \
    | tee >(go-junit-report > "${COVERAGEDIR}/${filename}-junit.xml") \
    && RC=$? || RC=$?

  if [[ ${RC} != 0 ]]; then
    if (( count < TEST_RETRY_COUNT )); then
      code_coverage "${1}" $((count+1))
    else
      echo "${1}" | tee "${COVERAGEDIR}/${filename}.err"
    fi
  fi

  #remove skipped tests from .cov file
  remove_skipped_tests_from_cov "${COVERAGEDIR}/${filename}.cov"
}

function wait_for_proc() {
  local num
  num=$(jobs -p | wc -l)
  while [[ "${num}" -gt ${MAXPROCS} ]]; do
    sleep 2
    num=$(jobs -p|wc -l)
  done
}

# For generating junit.xml files
go get github.com/jstemmer/go-junit-report

echo "Code coverage test (concurrency ${MAXPROCS})"
for P in $(go list "${DIR}" | grep -v vendor); do
  if [[ $(echo "${P}" | grep -q "${SKIPPED_TESTS_GREP_ARGS}") != '' ]]; then
    echo "Skipped ${P}"
    continue
  fi
  code_coverage "${P}" &
  wait_for_proc
done

wait

touch "${COVERAGEDIR}/empty"
mkdir -p "${OUT_DIR}"
pushd "${OUT_DIR}"

# Build the combined coverage files
go get github.com/wadey/gocovmerge
gocovmerge "${COVERAGEDIR}"/*.cov > coverage.cov
cat "${COVERAGEDIR}"/*.report > report.out
go tool cover -html=coverage.cov -o coverage.html

# Build the combined junit.xml
go get github.com/imsky/junit-merger/...
junit-merger "${COVERAGEDIR}"/*-junit.xml > junit.xml

popd

echo "Intermediate files were written to ${COVERAGEDIR}"
echo "Final reports are stored in ${OUT_DIR}"

if ls "${COVERAGEDIR}"/*.err 1> /dev/null 2>&1; then
  echo "The following tests had failed:"
  cat "${COVERAGEDIR}"/*.err 
  exit 1
fi
