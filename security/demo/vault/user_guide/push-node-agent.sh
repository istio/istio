#!/bin/bash

source ./config-environment.sh

# Select the cluster used for Vault CA demo
gcloud container clusters get-credentials $CLUSTER --zone $ZONE --project $PROJECT

kubectl apply -f ./example-pod-sa.yaml

example_pod_sa=$(kubectl get secret $(kubectl get serviceaccount example-pod-sa \
-o jsonpath={.secrets[0].name}) -o jsonpath={.data.token} | base64 --decode -)
echo "The SA of example-pod-sa is $example_pod_sa"
# Save the example_pod_sa, which is used when authenticating a pod at Citadel.
# When starting NodeAgent, example-pod-sa.jwt file will be read by NodeAgent.
echo -n "$example_pod_sa" > example-pod-sa.jwt

cp example-pod-sa.jwt ~/go/src/istio.io/istio/security/tests/integration/vaultTest/testdata/example-workload-pod-sa.jwt

pushd ${ISTIO_DIR}
# Authenticate to Container Registry,
gcloud auth configure-docker
# NodeAgent docker image is created from $GOPATH/src/istio.io/istio/security/docker/Dockerfile.node-agent-vault-test
# and pushed to the docker registry of the GCP project $PROJECT.
make docker.node-agent-vault-test
popd

