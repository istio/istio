#!/bin/bash

set -o errexit
set -o nounset
set -o pipefail
set -x

function usage() {
  echo "$0 \
    -t <tag name to apply to artifacts>"
  exit 1
}

# Initialize variables
TAG_NAME=""

# Handle command line args
while getopts t: arg ; do
  case "${arg}" in
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

if [ ! "${TAG_NAME}" ] ; then
  echo "-t is a required argument"
  usage
fi

# Cloud Builder checks out code in /workspace.
# We need to recreate the GOPATH directory structure
# for pilot to build correctly
function prepare_gopath() {
  [[ -z ${GOPATH} ]] && export GOPATH=/tmp/gopath
  mkdir -p ${GOPATH}/src/istio.io
  [[ -d ${GOPATH}/src/istio.io/istio/pilot ]] || ln -s ${PWD} ${GOPATH}/src/istio.io/pilot
  cd ${GOPATH}/src/istio.io/istio/pilot
  touch platform/kube/config
}

if [ ${PWD} != "${GOPATH}/src/istio.io/istio/pilot" ]; then
  prepare_gopath
fi

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

./bin/push-debian.sh -c opt -v "${TAG_NAME}" -p gs://istio-release/releases/"${TAG_NAME}"/deb
