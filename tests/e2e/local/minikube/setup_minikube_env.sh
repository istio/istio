#!/bin/bash

# Check if port forwarding is setup
# TODO: This ps | grep | grep is pretty hacky. Rewrite it.
# shellcheck disable=SC2009
if ! ps -eaf | grep "kubectl port-forward" | grep "kube-registry" | grep "5000:5000" > /dev/null; then
    echo "Port Forwarding not setup.Trying to set it up."
    POD=$(kubectl get po -n kube-system | grep kube-registry-v0 | awk '{print $1;}')
    # if ... &; then ... fi is a syntax error. Dropping the ';' is correct.
    if kubectl port-forward --namespace kube-system "$POD" 5000:5000 & then
        echo "Could not set up Port Forwarding. Something wrong with Minikube setup. Please check your setup and try again."
        exit 1
    fi
fi
echo "Port Forwarding is Setup"

# Check if minikube docker environment is setup.
if ! docker info | grep "localhost:5000" > /dev/null; then
    echo "Minikube docker environment not setup. Trying to set it up."
    if ! eval "$(minikube docker-env)"; then
        echo "Could not set up Minikube Docker Environment. Something wrong with Minikube setup. Please check your setup and try again."
        exit 1
    fi
fi
echo "Minikube docker environment is setup for this shell"
