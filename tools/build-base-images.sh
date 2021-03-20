#!/usr/bin/env bash

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

# This script runs go tests in a package, but each test is run individually. This helps
# isolate tests that are improperly depending on global state modification of other tests

set -ex

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
buildx-create

HUBS="${HUBS:?specify a space seperated list of hubs}"
TAG="${TAG:?specify a tag}"
DOCKER_TARGETS="${DOCKER_TARGETS:-docker.base docker.distroless docker.app_sidecar_base_debian_9 docker.app_sidecar_base_debian_10 docker.app_sidecar_base_ubuntu_xenial docker.app_sidecar_base_ubuntu_bionic docker.app_sidecar_base_ubuntu_focal docker.app_sidecar_base_centos_7 docker.app_sidecar_base_centos_8}"

# Verify that the specified TAG does not exist for the HUBS/TARGETS
# Will also fail if user doesn't have authorization to repository, but they shouldn't
# be able to push if no authorization.
# What other errors might happen that would be ignored ?
set +e
for hub in ${HUBS}
do
  for image in ${DOCKER_TARGETS#docker.}  # assume the image name is the target without the leading docker.
  do
    docker manifest inspect "$hub"/"$image":"$TAG" && exit 1 # will exit if it finds the manifest
  done
done
set -e

# For multi architecture building:
# See https://medium.com/@artur.klauser/building-multi-architecture-docker-images-with-buildx-27d80f7e2408 for more info
# * docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
# * docker buildx create --name multi-arch --platform linux/amd64,linux/arm64 --use
# * export DOCKER_ARCHITECTURES="linux/amd64,linux/arm64"
# Note: if you already have a container builder before running the qemu setup you will need to restart them

BUILDX_BAKE_EXTRA_OPTIONS="--no-cache --pull" DOCKER_TARGETS="${DOCKER_TARGETS}" make dockerx.pushx
