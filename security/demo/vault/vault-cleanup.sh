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
# k8s Vault service name
export VAULT_SERVICE=vault-server

gcloud container clusters get-credentials $CLUSTER --zone $CLUSTER_ZONE --project $GCP_PROJECT

kubectl delete deploy/${VAULT_DEPLOY} 
kubectl delete service/${VAULT_SERVICE}

# Delete service accounts
kubectl delete sa/vault-reviewer-sa
kubectl delete sa/vault-citadel-sa

# Delete pods
kubectl delete pods/vault-kubernetes

