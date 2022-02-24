#!/bin/bash
echo -e "kubectl apply -f mirror.yaml"
echo -e "cat mirror.yaml"
cat mirror.yaml
kubectl apply -f mirror.yaml
