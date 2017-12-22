#!/bin/bash
set -ex

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )

WORKSPACE=$SCRIPTPATH/..

cd ${WORKSPACE}

if [[ -z $SKIP_INIT ]];then
  bin/init.sh
fi

echo 'Running linters .... in advisory mode'

#TODO: after the new generation script is in, make sure we generate the exclude
docker run\
  -v $(pwd):/go/src/istio.io/istio\
  -w /go/src/istio.io/istio\
  gcr.io/mukai-istio/linter:bbcfb47f85643d4f5a7b1c092280d33ffd214c10\
  --config=./lintconfig_base.json \
  ./...

echo 'linters OK'

echo 'Checking licences'

bin/check_license.sh
echo 'licences OK'
