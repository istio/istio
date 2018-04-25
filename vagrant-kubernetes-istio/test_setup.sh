#!/bin/bash
# Start vagrant if not already started
vagrant up

# Setup HUB and TAG to talk to insecure local registry on VM.
HUB=10.10.0.2:5000 
TAG=latest 

# Remove old imges.
docker images -q |xargs docker rmi

# Make and Push images to insecure local registry on VM.
cd $ISTIO/istio
make docker HUB=10.10.0.2:5000 TAG=latest
make push HUB=10.10.0.2:5000 TAG=latest

# Verify images are pushed in repository.
echo "Check images present in repositories"
curl 10.10.0.2:5000/v2/_catalog -v


