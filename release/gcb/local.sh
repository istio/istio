#!/bin/bash
set -x
source gcb_lib.sh
ROOT=$(echo $( cd $(git rev-parse --show-cdup); pwd))
artifacts=(~/output/local)
NEW_VERSION="local.1"
DOCKER_HUB='utka/testing_local'
GOPATH="$(cd "$ROOT/../../.." && pwd)"
export GOPATH
echo gopath is "$GOPATH"

CB_VERIFY_CONSISTENCY=true


echo "Delete old builds"
rm -r ${artifacts} || echo
mkdir -p ${artifacts}

pushd ${ROOT}/../tools
  TOOLS_HEAD_SHA=$(git rev-parse HEAD)
popd

pushd $ROOT
  create_manifest_check_consistency "${artifacts}/manifest.txt"
popd
