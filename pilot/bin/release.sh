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

# Download and set the credentials for docker.io/istio hub
mkdir -p "${HOME}/.docker"
gsutil cp gs://istio-secrets/dockerhub_config.json.enc "${HOME}/.docker/config.json.enc"
gcloud kms decrypt \
       --ciphertext-file="${HOME}/.docker/config.json.enc" \
       --plaintext-file="${HOME}/.docker/config.json" \
       --location=global \
       --keyring=Secrets \
       --key=DockerHub

# Build istioctl binaries and upload to GCS
./bin/upload-istioctl -r -p gs://istio-release/releases/"${TAG_NAME}"/istioctl

./bin/push-docker -hub gcr.io/istio-io,docker.io/istio -tag "${TAG_NAME}"
