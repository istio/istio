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

# Check and setup Minikube environment
# shellcheck source=tests/e2e/local/minikube/setup_minikube_env.sh
. ./setup_minikube_env.sh

# Remove old images.
read -p "Do you want to delete old docker images tagged localhost:5000/*:e2e[default: no]: " -r update
delete_images=${update:-"no"}
if [[ $delete_images = *"y"* ]] || [[ $delete_images = *"Y"* ]]; then
  docker images localhost:5000/*:e2e -q | xargs docker rmi -f
fi

# Make and Push images to insecure local registry on VM.
# Set GOOS=linux to make sure linux binaries are built on macOS
cd "$ISTIO/istio" || exit
GOOS=linux make docker HUB=localhost:5000 TAG=e2e

echo "Setup done."
