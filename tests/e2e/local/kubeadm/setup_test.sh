#!/bin/bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# Remove old imges.
docker images 10.10.0.2:5000/*:latest -q | xargs docker rmi

# Make and Push images to insecure local registry on VM.
# Set GOOS=linux to make sure linux binaries are built on MacOS
cd "$ISTIO/istio" || exit
GOOS=linux make docker HUB=10.10.0.2:5000 TAG=latest
GOOS=linux make push HUB=10.10.0.2:5000 TAG=latest

# Verify images are pushed in repository.
echo "Check images present in repositories"
curl 10.10.0.2:5000/v2/_catalog
echo "Setup done."
