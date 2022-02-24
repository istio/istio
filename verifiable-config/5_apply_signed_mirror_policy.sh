#!/bin/bash
echo -e "kubectl apply -f mirror_signed.yaml"
echo -e "cat mirror_signed.yaml"
cat mirror_signed.yaml
kubectl apply -f mirror_signed.yaml
