#!/bin/bash
echo -e "kubectl apply -f authz/authz_signed.yaml"
echo -e "cat authz/authz_signed.yaml"
cat authz/authz_signed.yaml
kubectl apply -f authz/authz_signed.yaml
