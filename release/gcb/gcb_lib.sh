#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.


# This script is meant to be sourced, has a set of functions used by scripts on gcb
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/docker_tag_push_lib.sh
source "${SCRIPTPATH}/docker_tag_push_lib.sh"

#sets GITHUB_KEYFILE to github auth file
function github_keys() {
  GITHUB_KEYFILE="${GITHUB_TOKEN_FILE}"
  export GITHUB_KEYFILE
  
  if [[ -n "$CB_TEST_GITHUB_TOKEN_FILE_PATH" ]]; then
    local LOCAL_DIR
    LOCAL_DIR="$(mktemp -d /tmp/github.XXXX)"
    local KEYFILE_TEMP
    KEYFILE_TEMP="$LOCAL_DIR/keyfile.txt"
    GITHUB_KEYFILE="${KEYFILE_TEMP}"
    gsutil -q cp "gs://${CB_TEST_GITHUB_TOKEN_FILE_PATH}" "${KEYFILE_TEMP}"
  fi
}

function create_manifest_check_consistency() {
  local MANIFEST_FILE=$1

  local ISTIO_REPO_SHA
  local PROXY_REPO_SHA
  local CNI_REPO_SHA
  local API_REPO_SHA
  ISTIO_REPO_SHA=$(git rev-parse HEAD)
  CNI_REPO_SHA=$(grep CNI_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
  PROXY_REPO_SHA=$(grep PROXY_REPO_SHA istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
  API_REPO_SHA=$(grep "istio\.io/api" go.mod | cut -f 3 -d '-')
  if [ -z "${ISTIO_REPO_SHA}" ] || [ -z "${API_REPO_SHA}" ] || [ -z "${PROXY_REPO_SHA}" ] || [ -z "${CNI_REPO_SHA}" ] ; then
    echo "ISTIO_REPO_SHA:$ISTIO_REPO_SHA API_REPO_SHA:$API_REPO_SHA PROXY_REPO_SHA:$PROXY_REPO_SHA CNI_REPO_SHA:$CNI_REPO_SHA some shas not found"
    exit 8
  fi
  pushd ../api || exit
    API_SHA=$(git rev-parse "${API_REPO_SHA}")
  popd || exit
cat << EOF > "${MANIFEST_FILE}"
istio ${ISTIO_REPO_SHA}
proxy ${PROXY_REPO_SHA}
api ${API_SHA}
cni ${CNI_REPO_SHA}
tools ${TOOLS_HEAD_SHA}
EOF


  if [[ "${CB_VERIFY_CONSISTENCY}" == "true" ]]; then
     pushd ../proxy || exit
       PROXY_API_SHA=$(grep ISTIO_API istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"')
     popd || exit
     if [[ "$PROXY_API_SHA" != "$API_REPO_SHA"* ]]; then
       echo "inconsistent shas PROXY_API_SHA $PROXY_API_SHA !=   $API_REPO_SHA   API_REPO_SHA" 1>&2
       exit 17
     fi
  fi
}

function make_istio() {
  # Should be called from istio/istio repo.
  local OUTPUT_PATH=$1
  local DOCKER_HUB=$2
  local REL_DOCKER_HUB=$3
  TAG=$4
  export TAG
  local BRANCH=$5
  ISTIO_OUT=$(make DEBUG=0 where-is-out)
  VERSION="${TAG}"
  export VERSION
  MAKE_TARGETS=(istio-archive)
  MAKE_TARGETS+=(sidecar.deb)
  
  CB_BRANCH=${BRANCH} VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB="${DOCKER_HUB}" HUB="${DOCKER_HUB}" make "${MAKE_TARGETS[@]}"
  mkdir -p "${OUTPUT_PATH}/deb"
  sha256sum "${ISTIO_OUT}/istio-sidecar.deb" > "${OUTPUT_PATH}/deb/istio-sidecar.deb.sha256"
  cp        "${ISTIO_OUT}/istio-sidecar.deb"   "${OUTPUT_PATH}/deb/"
  cp        "${ISTIO_OUT}"/archive/istio-*z*   "${OUTPUT_PATH}/"
  
  rm -r "${ISTIO_OUT}/docker" || true
  BUILD_DOCKER_TARGETS=(docker.save)

  CB_BRANCH=${BRANCH} VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} make "${BUILD_DOCKER_TARGETS[@]}"

  # preserve the source from the root of the code
  pushd "${ROOT}/../../../.." || exit
    pwd
    # tar the source code
    if [ -z "${LOCAL_BUILD+x}" ]; then
      tar -czf "${OUTPUT_PATH}/source.tar.gz" go src --exclude go/out --exclude go/bin 
    else
      tar -cvzf "${OUTPUT_PATH}/source.tar.gz" go/src/istio.io/istio go/src/istio.io/api go/src/istio.io/cni go/src/istio.io/proxy
    fi
  popd || exit

  cp -r "${ISTIO_OUT}/docker" "${OUTPUT_PATH}/"
  go run tools/license/get_dep_licenses.go --branch "${BRANCH}" > LICENSES.txt

  # Add extra artifacts for legal compliance. The caller of this script can
  # optionally set their EXTRA_ARTIFACTS environment variable to an arbitrarily
  # long list of space-delimited filepaths --- and each artifact would get
  # injected into the Docker image.
  add_extra_artifacts_to_tar_images \
    "${DOCKER_HUB}" \
    "${TAG}" \
    "${OUTPUT_PATH}" \
    "${DOCKER_HUB}" \
    "${EXTRA_ARTIFACTS:-$PWD/LICENSES.txt}"

  # log where git thinks the build might be dirty
  git status
}

function make_cni() {
  #Handle CNI artifacts. Expects to be called from istio/cni repo.
  local OUTPUT_PATH=$1
  local DOCKER_HUB=$2
  local REL_DOCKER_HUB=$3
  TAG=$4
  export TAG
  local BRANCH=$5
  CNI_OUT=$(make DEBUG=0 where-is-out)
  rm -r "${CNI_OUT}/docker" || true
  VERSION="${TAG}"
  export VERSION
  # CNI version strategy is to have CNI run lock step with Istio i.e. CB_VERSION
  CB_BRANCH=${BRANCH} VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB="${DOCKER_HUB}" HUB="${DOCKER_HUB}" make build
  
  CB_BRANCH=${BRANCH} VERBOSE=1 DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} make docker.save || exit 1

  cp -r "${CNI_OUT}/docker" "${OUTPUT_PATH}/"
  go run ../istio/tools/license/get_dep_licenses.go --branch "${BRANCH}" > LICENSES.txt
  add_extra_artifacts_to_tar_images \
      "${DOCKER_HUB}" \
      "${TAG}" \
      "${OUTPUT_PATH}" \
      "${DOCKER_HUB}" \
      "${EXTRA_ARTIFACTS_CNI:-$PWD/LICENSES.txt}"
  git status
}

