#!/usr/bin/env bash
set -e
set -u
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR


echo "Code coverage test"
echo "" > coverage.txt
for d in $(go list ./... | grep -v vendor); do
    go test -coverprofile=profile.out $d
    if [ -f profile.out ]; then
        cat profile.out >> coverage.txt
        rm profile.out
    fi
done

if [ ! -z "${UPLOAD_TOKEN:-}" ]; then
  curl -s https://codecov.io/bash | CI_JOB_ID="${JOB_NAME}" CI_BUILD_ID="${BUILD_NUMBER}" bash /dev/stdin \
      -K -Z -B ${PULL_BASE_REF} -C ${GIT_SHA} -P ${PULL_NUMBER} -t ${UPLOAD_TOKEN}
fi
