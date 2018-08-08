#!/bin/bash

# Start vagrant if not already started
vagrant up

# Remove old imges.
docker images 10.10.0.2:5000/*:latest -q | xargs docker rmi

# Make and Push images to insecure local registry on VM.
# Set GOOS=linux to make sure linux binaries are built on macOS
cd "$ISTIO/istio" || exit
GOOS=linux make docker HUB=10.10.0.2:5000 TAG=latest
GOOS=linux make push HUB=10.10.0.2:5000 TAG=latest

# Verify images are pushed in repository.
echo "Check images present in repositories"
curl 10.10.0.2:5000/v2/_catalog
echo "Setup done."
