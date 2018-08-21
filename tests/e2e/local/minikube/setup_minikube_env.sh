#!/bin/bash

# Check if port forwarding is setup
ps -eaf | grep "kubectl port-forward" | grep "kube-registry" | grep "5000:5000" > /dev/null
if [ $? -ne 0 ]; then
    echo "Port Forwarding not setup.Trying to set it up."
    POD=`kubectl get po -n kube-system | grep kube-registry-v0 | awk '{print $1;}'`
    kubectl port-forward --namespace kube-system $POD 5000:5000 &
    if [ $? -ne 0 ]; then
        echo "Could not set up Port Forwarding. Something wrong with Minikube setup. Please check your setup and try again."
        exit 1
    fi
fi
echo "Port Forwarding is Setup"

# Check if minikube docker environment is setup.
docker info | grep "localhost:5000" > /dev/null
if [ $? -ne 0 ]; then
    echo "Minikube docker environment not setup. Trying to set it up."
    eval $(minikube docker-env)
    if [ $? -ne 0 ]; then
        echo "Could not set up Minikube Docker Environment. Something wrong with Minikube setup. Please check your setup and try again."
        exit 1
    fi
fi
echo "Minikube docker environment is setup for this shell"
