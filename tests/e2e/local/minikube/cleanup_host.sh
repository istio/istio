#!/bin/bash

# Getting docker back to it's previous setup.
eval "$(docker-machine env -u)"

# Removing port forwarding that was setup.
kill "$(pgrep "kubectl port-forward")" > /dev/null 2>&1

# Delete namespace istio-system if exists
kubectl delete namespace istio-system > /dev/null 2>&1

