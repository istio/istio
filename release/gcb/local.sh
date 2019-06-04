#!/bin/bash
set -x

# shellcheck disable=SC1091
source gcb_lib.sh
ROOT=$(cd "$(git rev-parse --show-cdup)" && pwd || return)
artifacts="$HOME/output/local"
export NEW_VERSION="local.1"
export DOCKER_HUB='utka/testing_local'
GOPATH=$(cd "$ROOT/../../.." && pwd)
export GOPATH
echo "gopath is $GOPATH"

export CB_VERIFY_CONSISTENCY=true

echo "Delete old builds"
rm -r "${artifacts}" || echo
mkdir -p "${artifacts}"

pushd "${ROOT}/../tools" || exit
  TOOLS_HEAD_SHA=$(git rev-parse HEAD)
  export TOOLS_HEAD_SHA
popd || return

pushd "$ROOT" || exit
  create_manifest_check_consistency "${artifacts}/manifest.txt"
popd || return
