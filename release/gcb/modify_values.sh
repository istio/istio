#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

#uses CB_DOCKER_HUB CB_GCS_BUILD_PATH CB_VERSION"

# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"


function fix_values_yaml_worker() {
  local unzip_cmd
  unzip_cmd="$1"
  local zip_cmd
  zip_cmd="$2"
  local folder_name
  folder_name="$3"
  local tarball_name
  tarball_name="$4"
  local gcs_folder_path
  gcs_folder_path="$5"

  gsutil -q cp "${gcs_folder_path}/${tarball_name}" .
  eval    "$unzip_cmd"     "${tarball_name}"
  rm                       "${tarball_name}"

  sed -i "s|hub: gcr.io/istio-release|hub: ${CB_DOCKER_HUB}|g" ./"${folder_name}"/install/kubernetes/helm/istio*/values.yaml
  sed -i "s|tag: .*-latest-daily|tag: ${CB_VERSION}|g"         ./"${folder_name}"/install/kubernetes/helm/istio*/values.yaml

  eval "$zip_cmd" "${tarball_name}" "${folder_name}"
  sha256sum       "${tarball_name}" > "${tarball_name}.sha256"
  rm -rf                            "${folder_name}"

  gsutil -q cp "${tarball_name}"        "${gcs_folder_path}/${tarball_name}"
  gsutil -q cp "${tarball_name}.sha256" "${gcs_folder_path}/${tarball_name}.sha256"
  echo "DONE fixing  ${gcs_folder_path}/${tarball_name} with hub: ${CB_DOCKER_HUB} tag: ${CB_VERSION}"
}

function fix_values_yaml() {
  # called with params as shown below
  # fix_values_yaml unzip_cmd zip_cmd folder_name tarball_name

  fix_values_yaml_worker "$1" "$2" "$3" "$4" "gs://${CB_GCS_BUILD_PATH}"
}

mkdir modification-tmp
cd    modification-tmp || exit 2
ls -l
pwd

folder_name="istio-${CB_VERSION}"
# Linux
fix_values_yaml     "tar -zxf" "tar -zcf" "${folder_name}" "${folder_name}-linux.tar.gz"
# Mac
fix_values_yaml     "tar -zxf" "tar -zcf" "${folder_name}" "${folder_name}-osx.tar.gz"
# Windows
fix_values_yaml    "unzip -q" "zip -q -r" "${folder_name}" "${folder_name}-win.zip"

#filename | sha256 hash
#-------- | -----------
#[kubernetes.tar.gz](https://dl.k8s.io/v1.10.6/kubernetes.tar.gz) | `dbb1e757ea8fe5e82796db8604d3fc61f8b79ba189af8e3b618d86fcae93dfd0`
