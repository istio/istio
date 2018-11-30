#!/bin/bash

# Clean up existing deployment and services to be used for the Vault demo
kubectl delete deploy/${VAULT_DEPLOY}
kubectl delete service/${VAULT_SERVICE}
kubectl delete -f ./client/node-agent-pod-1.yaml
kubectl delete -f ./citadel/istio-citadel-vault-demo.yaml

# Delete service accounts
kubectl delete sa/vault-reviewer-sa
kubectl delete sa/vault-citadel-sa

# Delete pods
kubectl delete pods/vault-kubernetes


