#!/usr/bin/env bash
set -e
set -u
set -x
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
source $SCRIPTPATH/use_bazel_go.sh

ROOTDIR=$SCRIPTPATH/..
cd $ROOTDIR


echo "Code coverage test"
TMPDIR=$(mktemp -d)

i=0
# half the number of cpus seem to saturate
max=$[$(getconf _NPROCESSORS_ONLN)/2] 
num=0
for d in $(go list ./... | grep -v vendor); do
    i=$[$i+1]
    if [[ $d == *"bootstrapgen" ]];then
      echo "Skipped $d"
      continue
    fi
    echo $d
    go test -coverprofile=$TMPDIR/$i $d &
    num=$(jobs -p|wc -l)
    while [ $num -gt $max ]
    do
      sleep 2
      num=$(jobs -p|wc -l)
    done
done

wait

cat $TMPDIR/* > coverage.txt

if [ ! -z "${UPLOAD_TOKEN:-}" ]; then
  curl -s https://codecov.io/bash | CI_JOB_ID="${JOB_NAME}" CI_BUILD_ID="${BUILD_NUMBER}" bash /dev/stdin \
      -K -Z -B ${PULL_BASE_REF} -C ${GIT_SHA} -P ${PULL_NUMBER} -t ${UPLOAD_TOKEN}
fi
