#!/usr/bin/env bash
set -e
set -u
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
ROOTDIR=$(dirname $SCRIPTPATH)
cd $ROOTDIR

echo "Code coverage test"

OUT=coverage.txt
PROF=profile.out

echo "" > ${OUT}
for d in $(go list ./... | grep -v vendor); do
    #FIXME remove mixer tools exclusions after tests can be run without bazel
    if [[ $d == "istio.io/istio/tests"* || \
      $d == "istio.io/istio/mixer/tools/codegen"* ]];then
      echo "Skipped $d"
      continue
    fi

    go test -coverprofile=${PROF} $d

    if [ -f ${PROF} ]; then
        cat ${PROF} >> ${OUT}
        rm ${PROF}
    fi
done

