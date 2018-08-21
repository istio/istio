#!/bin/bash

# Getting docker back to it's previous setup.
eval "$(docker-machine env -u)"

# Removing port forwarding that was setup.
kill `ps -eaf | grep "kubectl port-forward" | awk '{print $2;}'` > /dev/null 2>&1

# Delete namespace istio-system if exists
kubectl delete namespace istio-system > /dev/null 2>&1

