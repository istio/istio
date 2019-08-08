#!/bin/bash
kubectl -n foo delete policy jwt-example
kubectl -n foo delete destinationrule httpbin
kubectl delete ns foo bar legacy