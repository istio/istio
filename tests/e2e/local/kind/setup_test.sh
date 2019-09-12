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

# Remove old images.
read -p "Do you want to delete old docker images tagged kind/*:e2e[default: no]: " -r update
delete_images=${update:-"no"}
if [[ $delete_images = *"y"* ]] || [[ $delete_images = *"Y"* ]]; then
  docker images "kind/*:e2e" -q | xargs docker rmi -f
fi

# Make images.
# Set GOOS=linux to make sure linux binaries are built on macOS
cd "$ISTIO/istio" || exit
GOOS=linux make docker HUB=kind TAG=e2e

function build_kind_images(){
	# Create a temp directory to store the archived images.
	TMP_DIR=$(mktemp -d)
	IMAGE_FILE="${TMP_DIR}"/image.tar

	# Archived local images and load it into KinD's docker daemon
	# Kubernetes in KinD can only access local images from its docker daemon.
	docker images kind/*:e2e | awk 'FNR>1 {print $1 ":" $2}' | xargs docker save -o "${IMAGE_FILE}"
	kind load --name e2e image-archive "${IMAGE_FILE}"

	# Delete the local tar images.
	rm -rf "${IMAGE_FILE}"
}

echo "Setting up HUB and TAG"
export HUB="kind"
export TAG="e2e"

build_kind_images
echo "KinD setup done."
