#!/bin/bash

# Before running this script, the following variables
# need to be configured based on your environment.
# GCP cluster name for running the Vault server
export CLUSTER=vault-server
# GCP cluster zone
export CLUSTER_ZONE=us-west1-b
# GCP project for the cluster
export GCP_PROJECT=endpoints-authz-test1
# k8s Vault deployment name
export VAULT_DEPLOY=vault-server
# k8s Vault docker image name
export VAULT_DOCKER_IMAGE=vault-test
# k8s Vault docker image tag
export VAULT_DOCKER_IMAGE_TAG=leitang
# Vault port number
export VAULT_PORT=8200

# Clean up previous deployment
./vault-cleanup.sh

gcloud container clusters get-credentials $CLUSTER --zone $CLUSTER_ZONE --project $GCP_PROJECT

# Vault docker image is created from $GOPATH/src/istio.io/istio/security/docker/Dockerfile.vault-test
# and has been pushed to the docker registry of the GCP project.
kubectl run ${VAULT_DEPLOY} --image-pull-policy='Always' --image=gcr.io/${GCP_PROJECT}/${VAULT_DOCKER_IMAGE}:${VAULT_DOCKER_IMAGE_TAG}

# Expose the vault deployment through LoadBalancer
kubectl expose deployment ${VAULT_DEPLOY} --type=LoadBalancer --port ${VAULT_PORT} --target-port ${VAULT_PORT}

while true
do
    echo "Wait 10 seconds to get the ingress IP of Vault."
    sleep 10 
    output=$(kubectl describe service ${VAULT_DEPLOY}|grep -i ingress)
    # if ingress output is non-empty, break
    if [ ! -z "$output" ] 
    then
        break
    fi
done

export VAULT_SERVER_IP=$(kubectl describe service ${VAULT_DEPLOY}|grep -i ingress|cut -d':' -f 2|sed 's/ //g')
export VAULT_ADDR="http://${VAULT_SERVER_IP}:${VAULT_PORT}"
echo "Vault address is $VAULT_ADDR"
