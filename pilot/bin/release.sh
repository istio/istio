#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

function usage() {
  echo "$0 \
    -i <gcloud project ID name> \
    -t <tag name to apply to artifacts>"
  exit 1
}

# Initialize variables
PROJECT_ID=""
TAG_NAME=""

# Handle command line args
while getopts i:t: arg ; do
  case "${arg}" in
    i) PROJECT_ID="${OPTARG}";;
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

if [ ! "${PROJECT_ID}" ] || [ ! "${TAG_NAME}" ] ; then
  echo "-i and -t are required arguments"
  usage
fi

# Build environment setup
mkdir -p /tmp/gopath/src/istio.io
ln -s /workspace /tmp/gopath/src/istio.io/pilot
cd /tmp/gopath/src/istio.io/pilot
touch platform/kube/config

# Build istioctl binaries and upload to GCS
./bin/upload-istioctl -r -p gs://"${PROJECT_ID}"/pilot/"${TAG_NAME}"

./bin/push-docker -hub gcr.io/"${PROJECT_ID}" -tag "${TAG_NAME}"
