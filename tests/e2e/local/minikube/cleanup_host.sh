#!/bin/bash

# Getting docker back to it's previous setup.
eval "$(docker-machine env -u)"

# Delete namespace istio-system if exists
kubectl delete namespace istio-system > /dev/null 2>&1

