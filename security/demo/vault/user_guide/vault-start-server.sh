#!/bin/bash

source ./config-environment.sh

cd $DIR

# Log in if you have not logged in yet
gcloud auth login

# Select the cluster used for Vault CA demo
gcloud container clusters get-credentials $CLUSTER --zone $ZONE --project $PROJECT 

# Grant cluster admin permissions to the current user (admin permissions are required to create the necessary RBAC rules for Istio).
kubectl create clusterrolebinding cluster-admin-binding --clusterrole=cluster-admin --user=$(gcloud config get-value core/account)

source ./vault-cleanup.sh

pushd ${ISTIO_DIR}
# Authenticate to Container Registry,
gcloud auth configure-docker
# Vault docker image is created from $GOPATH/src/istio.io/istio/security/docker/Dockerfile.vault-test
# and pushed to the docker registry of the GCP project $GCP_PROJECT.
make docker.vault-test
popd

# Create a deployment for Vault server
kubectl run ${VAULT_DEPLOY} --image-pull-policy='Always' --image=gcr.io/${PROJECT}/${VAULT_DOCKER_IMAGE}:${TAG}

# Expose the Vault deployment through LoadBalancer
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

# Print Vault status
vault status


