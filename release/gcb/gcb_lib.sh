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

#shellcheck disable=SC2164
SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
#shellcheck disable=SC1090
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
}

function make_istio() {
  # Should be called from istio/istio repo.
  local OUTPUT_PATH=$1
  local DOCKER_HUB=$2
  local REL_DOCKER_HUB=$3
  TAG=$4
  export TAG
  local BRANCH=$5
  ISTIO_OUT=$(make -f Makefile.core.mk DEBUG=0 where-is-out)
  VERSION="${TAG}"
  export VERSION
  IFS='/' read -ra REPO <<< "$REL_DOCKER_HUB"
  MAKE_TARGETS=(istio-archive)
  MAKE_TARGETS+=(sidecar.deb)

  CB_BRANCH=${BRANCH} DEBUG=0 ISTIO_DOCKER_HUB="${DOCKER_HUB}" HUB="${DOCKER_HUB}" make "${MAKE_TARGETS[@]}"
  mkdir -p "${OUTPUT_PATH}/deb"
  sha256sum "${ISTIO_OUT}/istio-sidecar.deb" > "${OUTPUT_PATH}/deb/istio-sidecar.deb.sha256"
  cp        "${ISTIO_OUT}/istio-sidecar.deb"   "${OUTPUT_PATH}/deb/"
  cp        "${ISTIO_OUT}"/archive/istio-*z*   "${OUTPUT_PATH}/"

  for file in "${ISTIO_OUT}"/archive/istioctl*.*; do
    sha256sum "${file}" > "$file.sha256"
  done
  cp        "${ISTIO_OUT}"/archive/istioctl*.tar.gz "${OUTPUT_PATH}/"
  cp        "${ISTIO_OUT}"/archive/istioctl*.zip    "${OUTPUT_PATH}/"
  cp        "${ISTIO_OUT}"/archive/istioctl*.sha256 "${OUTPUT_PATH}/"

  rm -r "${ISTIO_OUT}/docker" || true
  BUILD_DOCKER_TARGETS=(docker.save)

  CB_BRANCH=${BRANCH} DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} DOCKER_BUILD_VARIANTS="default distroless" make "${BUILD_DOCKER_TARGETS[@]}"

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

  # log where git thinks the build might be dirty
  git status

  pushd ../cni || exit
    #Handle CNI artifacts. Expects to be called from istio/cni repo.
    CNI_OUT=$(make -f Makefile.core.mk DEBUG=0 where-is-out)
    rm -r "${CNI_OUT}/docker" || true
    # CNI version strategy is to have CNI run lock step with Istio i.e. CB_VERSION
    CB_BRANCH=${BRANCH} DEBUG=0 ISTIO_DOCKER_HUB="${DOCKER_HUB}" HUB="${DOCKER_HUB}" make build

    CB_BRANCH=${BRANCH} DEBUG=0 ISTIO_DOCKER_HUB=${REL_DOCKER_HUB} HUB=${REL_DOCKER_HUB} make docker.save || exit 1

    cp -r "${CNI_OUT}/docker" "${OUTPUT_PATH}/"
    git status
  popd || exit
  # Add extra artifacts for legal compliance. The caller of this script can
  # optionally set their EXTRA_ARTIFACTS environment variable to an arbitrarily
  # long list of space-delimited filepaths --- and each artifact would get
  # injected into the Docker image.

  go run istio.io/tools/cmd/license-lint --report > LICENSES.txt
  add_extra_artifacts_to_tar_images \
    "${DOCKER_HUB}" \
    "${TAG}" \
    "${OUTPUT_PATH}" \
    "${REPO[1]}" \
    "${EXTRA_ARTIFACTS:-$PWD/LICENSES.txt}"
}

fix_values_yaml() {
  local VERSION="$1"
  local DOCKER_HUB="$2"
  update_helm "istio-${VERSION}-linux.tar.gz" "${VERSION}" "${DOCKER_HUB}"
  update_helm "istio-${VERSION}-osx.tar.gz" "${VERSION}" "${DOCKER_HUB}"
  update_helm "istio-${VERSION}-win.zip" "${VERSION}" "${DOCKER_HUB}"
}

function update_helm() {
  local tarball_name="$1"
  local VERSION="$2"
  local DOCKER_HUB="$3"
  if [[ ${tarball_name} == *.zip ]]; then
    local unzip_cmd="unzip -q"
    local zip_cmd="zip -q -r"
  else
    local unzip_cmd="tar -zxf"
    local zip_cmd="tar -zcf"
  fi
  if [ -z "${LOCAL_BUILD+x}" ]; then
    gsutil -q cp "gs://${CB_GCS_BUILD_PATH}/${tarball_name}" .
  fi
  eval    "$unzip_cmd"     "${tarball_name}"
  rm                       "${tarball_name}"
  # Update version string in yaml files.
  sed -i "s|hub: gcr.io/istio-release|hub: ${DOCKER_HUB}|g" ./"istio-${VERSION}"/install/kubernetes/helm/istio*/values.yaml ./"istio-${VERSION}"/install/kubernetes/helm/istio-cni/values_gke.yaml
  sed -i "s|tag: .*-latest-daily|tag: ${VERSION}|g"         ./"istio-${VERSION}"/install/kubernetes/helm/istio*/values.yaml ./"istio-${VERSION}"/install/kubernetes/helm/istio-cni/values_gke.yaml
  current_tag=$(grep "appVersion" ./"istio-${VERSION}"/install/kubernetes/helm/istio/Chart.yaml  | cut -d ' ' -f2)
  # The Helm version must follow SemVer 2. In the case $VERSION doesn't (e.g. "master-latest-daily"),
  # prepend "0.0.0-" to it to make it a valid pre-release SemVer 2 version.
  local helm_version="$VERSION"
  if [[ ! $helm_version =~ ^[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    helm_version="0.0.0-$helm_version"
  fi
  if [ "${current_tag}" != "${VERSION}" ]; then
    find ./"istio-${VERSION}"/install/kubernetes/helm -type f -exec sed -i "s/tag: ${current_tag}/tag: ${VERSION}/g" {} \;
    find ./"istio-${VERSION}"/install/kubernetes/helm -type f -exec sed -i "s/version: ${current_tag}/version: ${helm_version}/g" {} \;
    find ./"istio-${VERSION}"/install/kubernetes/helm -type f -exec sed -i "s/appVersion: ${current_tag}/appVersion: ${VERSION}/g" {} \;
    find ./"istio-${VERSION}"/install/kubernetes/helm -type f -exec sed -i "s/istio-release\/releases\/${current_tag}/istio-release\/releases\/${VERSION}/g" {} \;
  fi

  # replace prerelease with release location for istio.io repo
  if [ "${CB_PIPELINE_TYPE}" = "monthly" ]; then
    sed -i.bak "s:istio-prerelease/daily-build.*$:istio-release/releases/${VERSION}/charts:g" ./"istio-${VERSION}"/install/kubernetes/helm/istio/README.md
    rm -rf ./"istio-${VERSION}"/install/kubernetes/helm/istio/README.md.bak
    echo "Done replacing pre-released charts with released charts for istio.io repo"
  fi
  eval "$zip_cmd" "${tarball_name}" "istio-${VERSION}"
  sha256sum       "${tarball_name}" > "${tarball_name}.sha256"
  rm  -rf "istio-${VERSION}"
  if [ -z "${LOCAL_BUILD+x}" ]; then
    gsutil -q cp "${tarball_name}"        "gs://${CB_GCS_BUILD_PATH}/${tarball_name}"
    gsutil -q cp "${tarball_name}.sha256" "gs://${CB_GCS_BUILD_PATH}/${tarball_name}.sha256"
    echo "DONE fixing gs://${CB_GCS_BUILD_PATH}/${tarball_name} with hub: ${CB_DOCKER_HUB} tag: ${CB_VERSION}"
  fi
}

function create_charts() {
  local VERSION="$1"
  local OUTPUT="$2"
  local HELM_DIR="$3"
  local HELM_BUILD_DIR="$4"
  local BRANCH="$5"
  local DOCKER_HUB="$6"
  tar -zxf "istio-${VERSION}-linux.tar.gz"
  mkdir -vp "${OUTPUT}/istio"
  cp -R "./istio-${VERSION}/install" "${OUTPUT}/istio/install"

  # Charts to extract from repos
  CHARTS=(
    "${OUTPUT}/istio/install/kubernetes/helm/istio"
    "${OUTPUT}/istio/install/kubernetes/helm/istio-cni"
    "${OUTPUT}/istio/install/kubernetes/helm/istio-init"
  )

  # Prepare helm setup
  mkdir -vp "${HELM_DIR}"
  HELM="helm --home ${HELM_DIR}"
  ${HELM} init --client-only

  # Create a package for each charts and build the repo index.
  mkdir -vp "${HELM_BUILD_DIR}"
  for CHART_PATH in "${CHARTS[@]}"
  do
      ${HELM} package "${CHART_PATH}" -d "${HELM_BUILD_DIR}"
  done

  ${HELM} repo index "${HELM_BUILD_DIR}"
}

