#!/bin/bash

istioctl kube-inject \
    --injectConfigFile inject-config.yaml \
    --meshConfigFile mesh-config.yaml \
    --valuesFile inject-values.yaml \
    --filename httpbin1.yaml \
    | kubectl apply -f -
