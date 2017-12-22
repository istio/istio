#!/usr/bin/env bash
set -e
set -u
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$(dirname $SCRIPTPATH)
cd $ROOTDIR

echo "Code coverage test"

OUT=coverage.txt
PROF=profile.out

echo "mode: set" > ${OUT}
for d in $(go list ./... | grep -v vendor); do
    #FIXME remove mixer tools exclusions after tests can be run without bazel
    if [[ $d == "istio.io/istio/tests"* || \
      $d == "istio.io/istio/mixer/tools/codegen"* ]];then
      echo "Skipped $d"
      continue
    fi

    # do not stop even when a test fails
    go test -coverprofile=${PROF} $d || true

    if [ -f ${PROF} ]; then
        grep -v 'mode: ' ${PROF} >> ${OUT}
        rm ${PROF}
    fi
done
