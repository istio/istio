#!/bin/bash

kubectl delete policy default -n default || true
kubectl delete destinationrule default -n default || true
