#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"
source gcb_lib.sh

# This script updates helm config files and add helm charts to the release tarballs.


# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

function fix_values_yaml() {

  gsutil -q cp "gs://${CB_GCS_BUILD_PATH}/${tarball_name}" .

  gsutil -q cp "${tarball_name}"        "gs://${CB_GCS_BUILD_PATH}/${tarball_name}"
  gsutil -q cp "${tarball_name}.sha256" "gs://${CB_GCS_BUILD_PATH}/${tarball_name}.sha256"
  echo "DONE fixing gs://${CB_GCS_BUILD_PATH}/${tarball_name} with hub: ${CB_DOCKER_HUB} tag: ${CB_VERSION}"
}

mkdir -p modification-tmp
cd    modification-tmp || exit 2
ls -l
pwd

fix_values_yaml ${CB_VERSION} ${CB_DOCKER_HUB}

#filename | sha256 hash
#-------- | -----------
#[kubernetes.tar.gz](https://dl.k8s.io/v1.10.6/kubernetes.tar.gz) | `dbb1e757ea8fe5e82796db8604d3fc61f8b79ba189af8e3b618d86fcae93dfd0`
