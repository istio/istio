#!/bin/bash

# Delete namespace istio-system if exists
kubectl delete namespace istio-system > /dev/null 2>&1

