#!/bin/bash

# Check and setup Minikube environment
# shellcheck source=tests/e2e/local/minikube/setup_minikube_env.sh
. ./setup_minikube_env.sh

# Remove old imges.
read -p "Do you want to delete old docker images tagged localhost:5000/*:latest[default: no]: " -r update
delete_images=${update:-"no"}
if [[ $delete_images = *"y"* ]] || [[ $delete_images = *"Y"* ]]; then
  docker images localhost:5000/*:latest -q | xargs docker rmi -f
fi

# Make and Push images to insecure local registry on VM.
# Set GOOS=linux to make sure linux binaries are built on macOS
cd "$ISTIO/istio" || exit
GOOS=linux make docker HUB=localhost:5000 TAG=latest
GOOS=linux make push HUB=localhost:5000 TAG=latest

# Verify images are pushed in repository.
echo "Check images present in repositories"
curl localhost:5000/v2/_catalog
echo "Setup done."
