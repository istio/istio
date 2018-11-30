#!/bin/bash

source ./config-environment.sh

# Select the cluster used for Vault CA demo
gcloud container clusters get-credentials $CLUSTER --zone $ZONE --project $PROJECT 

pushd ${ISTIO_DIR}
# Authenticate to Container Registry,
gcloud auth configure-docker
# Vault docker image is created from $GOPATH/src/istio.io/istio/security/docker/Dockerfile.vault-test
# and pushed to the docker registry of the GCP project $PROJECT.
make docker.citadel-vault-test-1
popd

