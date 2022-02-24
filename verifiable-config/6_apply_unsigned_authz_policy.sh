#!/bin/bash
echo -e "kubectl apply -f authz/authz.yaml"
echo -e "cat authz/authz.yaml"
cat authz/authz.yaml
kubectl apply -f authz/authz.yaml
