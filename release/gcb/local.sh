#!/bin/bash
set -x
set -o errexit

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh disable=1091
source "${SCRIPTPATH}/gcb_lib.sh"

function check_prerequisites() {
  for dependency in "tools" "api" "proxy" "cni"; do
    if [ ! -d "$SCRIPTPATH/../../../$dependency" ]; then
      echo "Repo missing: istio.io/$dependency."
      exit 1
    fi
  done

  # On mac, brew install gnu-tar gnu-cp
  if [ "$(uname -s)" = Darwin ]; then
    if [ ! "$(command -v gtar)" ]; then
      echo "Could not find gtar."
      exit 1
    fi
    if [ ! "$(command -v gcp)" ]; then
      echo "Could not find gcp."
      exit 1
    fi
    export CP="gcp"
    export TAR="gtar"
  fi
}

check_prerequisites
ROOT=$(cd "$(git rev-parse --show-cdup)" && pwd || return)
artifacts="$HOME/output/local"
export NEW_VERSION=${TAG:-}
export DOCKER_HUB=${DOCKER_HUB:-}
if [ -z "${NEW_VERSION}" ]; then
   echo "Provide or export the tag for the images. TAG=my_amazing_tag."
   exit 1
fi
if [ -z "${DOCKER_HUB}" ]; then
   echo "Provide or export the docker hub. DOCKER_HUB=docker.io/my_awesome_hub."
   exit 1
fi
GOPATH=$(cd "$ROOT/../../.." && pwd)
LOCAL_BUILD=true
export LOCAL_BUILD
export GOPATH
echo "gopath is $GOPATH"

CURRENT_BRANCH=$(git symbolic-ref --short HEAD)
CNI_BRANCH=${CNI_BRANCH:-$CURRENT_BRANCH}
export CB_VERIFY_CONSISTENCY=${VERIFY_CONSISTENCY:-true}
echo "Delete old builds"
rm -rf "${artifacts}" || echo
mkdir -p "${artifacts}"

pushd "${ROOT}/../tools" || exit
  TOOLS_HEAD_SHA=$(git rev-parse HEAD)
  export TOOLS_HEAD_SHA
popd || return

pushd "${ROOT}" || exit
  create_manifest_check_consistency "${artifacts}/manifest.txt"
  make_istio "${artifacts}" "${DOCKER_HUB}" "${DOCKER_HUB}" "${NEW_VERSION}" "${CNI_BRANCH}"
popd || return

docker_tag_images  "${DOCKER_HUB}" "${NEW_VERSION}" "${artifacts}"
docker_push_images "${DOCKER_HUB}" "${NEW_VERSION}" "${artifacts}"

pushd "${artifacts}" || exit
  fix_values_yaml "${NEW_VERSION}" "${DOCKER_HUB}"
popd || exit

pushd "${artifacts}" || exit
  create_charts "${NEW_VERSION}" "${artifacts}" "${artifacts}"/helm "${artifacts}"/charts "${CNI_BRANCH}" "${DOCKER_HUB}"
  rm -r "${artifacts}"/helm 
  rm -rf "${artifacts}"/cni
  rm -r "${artifacts}"/istio
  rm -r "${artifacts}"/istio-"${NEW_VERSION}"
popd || exit
