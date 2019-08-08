#!/bin/bash
kubectl delete meshpolicy default
kubectl delete destinationrules httpbin-legacy -n legacy
kubectl delete destinationrules api-server -n istio-system
kubectl delete destinationrules default -n istio-system