#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

while getopts h:t:p:v: arg ; do
  case "${arg}" in
    h) HUB="${OPTARG}";;
    t) TAG="${OPTARG}";;
    p) GCS_PATH="${OPTARG}";;
    v) VERSION="${OPTARG}";;
    *) exit 1;;
  esac
done

function fix_values_yaml() {
  local unzip_cmd
  unzip_cmd="$1"
  local zip_cmd
  zip_cmd="$2"
  local folder_name
  folder_name="$3"
  local tarball_name
  tarball_name="$4"

  gsutil cp "${GCS_PATH}/${tarball_name}" .
  eval $unzip_cmd        ${tarball_name}
  rm                     ${tarball_name}

  sed -i     "s|hub: gcr.io/istio-release|hub: ${HUB}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml
  sed -i "s|tag: release-1.0-latest-daily|tag: ${TAG}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml

  eval $zip_cmd ${tarball_name} ${folder_name}
  rm -rf                       "${folder_name}"

  gsutil cp "${tarball_name}" "${GCS_PATH}/${tarball_name}"
  gsutil cp "${tarball_name}" "${GCS_PATH}/docker.io/${tarball_name}"
  gsutil cp "${tarball_name}" "${GCS_PATH}/gcr.io/${tarball_name}"
}

rm -rf modification-tmp
mkdir  modification-tmp
cd     modification-tmp || exit 2
ls -l
pwd

folder_name="istio-${VERSION}"
# Linux
fix_values_yaml     "tar -zxvf" "tar -zcvf" "{folder_name}" "${folder_name}-linux.tar.gz"
# Mac
fix_values_yaml     "tar -zxvf" "tar -zcvf" "{folder_name}" "${folder_name}-osx.tar.gz"
# Windows
cp /home/airflow/gcs/data/zip     ./zip
cp /home/airflow/gcs/data/unzip ./unzip
chmod               u+x ./unzip   ./zip
fix_values_yaml          "unzip"   "zip -r" "{folder_name}" "${folder_name}-win.zip"

cd ..
rm -rf modification-tmp
exit 0

filename | sha256 hash
-------- | -----------
[kubernetes.tar.gz](https://dl.k8s.io/v1.10.6/kubernetes.tar.gz) | `dbb1e757ea8fe5e82796db8604d3fc61f8b79ba189af8e3b618d86fcae93dfd0`
