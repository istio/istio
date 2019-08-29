#!/bin/bash
kubectl create ns foo
kubectl apply -f <(istioctl kube-inject -f @samples/httpbin/httpbin.yaml@) -n foo
kubectl apply -f <(istioctl kube-inject -f @samples/sleep/sleep.yaml@) -n foo
kubectl create ns bar
kubectl apply -f <(istioctl kube-inject -f @samples/httpbin/httpbin.yaml@) -n bar
kubectl apply -f <(istioctl kube-inject -f @samples/sleep/sleep.yaml@) -n bar
kubectl create ns legacy
kubectl apply -f @samples/sleep/sleep.yaml@ -n legacy
