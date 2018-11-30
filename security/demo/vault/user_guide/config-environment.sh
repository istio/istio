#!/bin/bash

# Create a new GKE cluster for running the demo and modify the following variables
# based on your GKE project and cluster setting.
# Your GKE project name
export PROJECT=YOUR-PROJECT-NAME
# Your GKE cluster zone
export ZONE=YOUR-CLUSTER-ZONE
# Your newly created GKE cluster name
export CLUSTER=YOUR-CLUSTER-NAME

# ISTIO_DIR points to your cloned istio directory
export ISTIO_DIR=~/go/src/istio.io/istio
# DIR points to your directory containing the user_guide
export DIR=${ISTIO_DIR}/security/demo/vault/user_guide
# HUB points to your gcr.io HUB for custom docker container
# builds.
export HUB="gcr.io/${PROJECT}"
# The Istio Docker build system will build images with a tag composed of
# $USER and timestamp. The codebase doesn't consistently use the same timestamp
# tag. To simplify development the development process when later using
# updateVersion.sh you may find it helpful to set TAG to something consistent
# such as $USER.
export TAG=$USER
# k8s Vault deployment name
export VAULT_DEPLOY=vault-server
# k8s Vault service name
export VAULT_SERVICE=vault-server
# k8s Vault deployment name
export VAULT_DEPLOY=vault-server
# k8s Vault docker image name
export VAULT_DOCKER_IMAGE=vault-test
# Vault port number
export VAULT_PORT=8200


