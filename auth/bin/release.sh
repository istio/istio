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
while getopts i:t: arg ; do
  case "${arg}" in
    t) TAG_NAME="${OPTARG}";;
    *) usage;;
  esac
done

mkdir -p $HOME/.docker
gsutil cp gs://istio-secrets/dockerhub_config.json.enc $HOME/.docker/config.json.enc
gcloud kms decrypt \
       --ciphertext-file=$HOME/.docker/config.json.enc \
       --plaintext-file=$HOME/.docker/config.json \
       --location=global \
       --keyring=Secrets \
       --key=DockerHub

./bin/push-docker.sh \
    -h gcr.io/istio-io,docker.io/istio \
    -t $TAG_NAME
