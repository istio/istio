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

rm -rf modification-tmp
mkdir modification-tmp
cd modification-tmp || exit

folder_name="istio-${VERSION}"

ls -l
pwd

# Linux
tarball_name="${folder_name}-linux.tar.gz"
gsutil cp "${GCS_PATH}/${tarball_name}" .
tar -zxvf ${tarball_name}
rm "${tarball_name}"

sed -i "s|hub: gcr.io/istio-release|hub: ${HUB}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml
sed -i "s|tag: release-1.0-latest-daily|tag: ${TAG}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml

tar -zcvf "${tarball_name}" "${folder_name}"
rm -rf "${folder_name}"

gsutil cp "${tarball_name}" "${GCS_PATH}/${tarball_name}"
gsutil cp "${tarball_name}" "${GCS_PATH}/docker.io/${tarball_name}"
gsutil cp "${tarball_name}" "${GCS_PATH}/gcr.io/${tarball_name}"

# Mac
tarball_name="${folder_name}-osx.tar.gz"
gsutil cp "${GCS_PATH}/${tarball_name}" .
tar -zxvf ${tarball_name}
rm "${tarball_name}"

sed -i "s|hub: gcr.io/istio-release|hub: ${HUB}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml
sed -i "s|tag: release-1.0-latest-daily|tag: ${TAG}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml

tar -zcvf "${tarball_name}" "${folder_name}"
rm -rf "${folder_name}"

gsutil cp "${tarball_name}" "${GCS_PATH}/${tarball_name}"
gsutil cp "${tarball_name}" "${GCS_PATH}/docker.io/${tarball_name}"
gsutil cp "${tarball_name}" "${GCS_PATH}/gcr.io/${tarball_name}"

# Windows
cp /home/airflow/gcs/data/zip ./zip
cp /home/airflow/gcs/data/unzip ./unzip
chmod u+x ./zip ./unzip

tarball_name="${folder_name}-win.zip"
gsutil cp "${GCS_PATH}/${tarball_name}" .
./unzip ${tarball_name}
rm "${tarball_name}"

sed -i "s|hub: gcr.io/istio-release|hub: ${HUB}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml
sed -i "s|tag: release-1.0-latest-daily|tag: ${TAG}|g" ./${folder_name}/install/kubernetes/helm/istio*/values.yaml

./zip -r "${tarball_name}" "${folder_name}"
rm -rf "${folder_name}"

gsutil cp "${tarball_name}" "${GCS_PATH}/${tarball_name}"
gsutil cp "${tarball_name}" "${GCS_PATH}/docker.io/${tarball_name}"
gsutil cp "${tarball_name}" "${GCS_PATH}/gcr.io/${tarball_name}"

cd ..
rm -rf modification-tmp
