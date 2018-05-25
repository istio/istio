#!/bin/bash

# Remove old imges.
docker images localhost:5000/*:latest -q | xargs docker rmi -f

# Make and Push images to insecure local registry on VM.
# Set GOOS=linux to make sure linux binaries are built on macOS
cd $ISTIO/istio
GOOS=linux make docker HUB=localhost:5000 TAG=latest
GOOS=linux make push HUB=localhost:5000 TAG=latest

# Verify images are pushed in repository.
echo "Check images present in repositories"
curl localhost:5000/v2/_catalog
echo "Setup done."
