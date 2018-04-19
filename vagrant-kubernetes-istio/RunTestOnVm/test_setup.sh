#!/bin/bash

# Setup HUB and TAG to talk to insecure local registry on VM.
HUB=10.10.0.2:5000 
TAG=latest 

# Make and Push images to insecure local registry on VM.
# Save current path
curpath="$PWD"

cd $ISTIO/istio
echo "Build docker images."
make docker HUB=10.10.0.2:5000 TAG=latest
echo "Push docker images to local registry in VM."
make push HUB=10.10.0.2:5000 TAG=latest
cd $curpath

# Verify images are pushed in repository.
echo "$(tput setaf 1)Please make sure images present in repositories$(tput sgr 0)"
curl 10.10.0.2:5000/v2/_catalog -v


