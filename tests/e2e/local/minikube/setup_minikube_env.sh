#!/bin/bash

# Check if minikube docker environment is setup.
if ! docker info | grep "localhost:5000" > /dev/null; then
    echo "Minikube docker environment not setup. Trying to set it up."
    if ! eval "$(minikube docker-env)"; then
        echo "Could not set up Minikube Docker Environment. Something wrong with Minikube setup. Please check your setup and try again."
        exit 1
    fi
fi
echo "Minikube docker environment is setup for this shell"
