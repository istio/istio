#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"


# This script updates helm config files and add helm charts to the release tarballs.


# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

function fix_values_yaml() {
  local tarball_name="$1"
  if [[ ${tarball_name} == *.zip ]]; then
    local unzip_cmd="unzip -q"
    local zip_cmd="zip -q -r"
  else
    local unzip_cmd="tar -zxf"
    local zip_cmd="tar -zcf"
  fi

  gsutil -q cp "gs://${CB_GCS_BUILD_PATH}/${tarball_name}" .
  eval    "$unzip_cmd"     "${tarball_name}"
  rm                       "${tarball_name}"

  # Update version string in yaml files.
  sed -i "s|hub: gcr.io/istio-release|hub: ${CB_DOCKER_HUB}|g" ./"istio-${CB_VERSION}"/install/kubernetes/helm/istio*/values.yaml
  sed -i "s|tag: .*-latest-daily|tag: ${CB_VERSION}|g"         ./"istio-${CB_VERSION}"/install/kubernetes/helm/istio*/values.yaml

  # replace prerelease with release location for istio.io repo
  if [ "${CB_PIPELINE_TYPE}" = "monthly" ]; then
    sed -i.bak "s:istio-prerelease/daily-build.*$:istio-release/releases/${CB_VERSION}/charts:g" ./"istio-${CB_VERSION}"/install/kubernetes/helm/istio/README.md
    rm -rf ./"istio-${CB_VERSION}"/install/kubernetes/helm/istio/README.md.bak
    echo "Done replacing pre-released charts with released charts for istio.io repo"
  fi

  eval "$zip_cmd" "${tarball_name}" "istio-${CB_VERSION}"
  sha256sum       "${tarball_name}" > "${tarball_name}.sha256"
  rm  -rf "istio-${CB_VERSION}"

  gsutil -q cp "${tarball_name}"        "gs://${CB_GCS_BUILD_PATH}/${tarball_name}"
  gsutil -q cp "${tarball_name}.sha256" "gs://${CB_GCS_BUILD_PATH}/${tarball_name}.sha256"
  echo "DONE fixing gs://${CB_GCS_BUILD_PATH}/${tarball_name} with hub: ${CB_DOCKER_HUB} tag: ${CB_VERSION}"
}

mkdir -p modification-tmp
cd    modification-tmp || exit 2
ls -l
pwd

# Linux
fix_values_yaml  "istio-${CB_VERSION}-linux.tar.gz"
# Mac
fix_values_yaml  "istio-${CB_VERSION}-osx.tar.gz"
# Windows
fix_values_yaml  "istio-${CB_VERSION}-win.zip"

#filename | sha256 hash
#-------- | -----------
#[kubernetes.tar.gz](https://dl.k8s.io/v1.10.6/kubernetes.tar.gz) | `dbb1e757ea8fe5e82796db8604d3fc61f8b79ba189af8e3b618d86fcae93dfd0`
