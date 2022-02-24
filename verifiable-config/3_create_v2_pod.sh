#!/bin/bash

echo -e "istioctl kube-inject --filename httpbin2.yaml | kubectl apply -f -"

istioctl kube-inject --filename httpbin2.yaml | kubectl apply -f -
