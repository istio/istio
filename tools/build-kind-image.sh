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

# This script builds a multi-arch kind node image
# Largely copied from https://github.com/kubernetes-sigs/kind/blob/2a1e9df91fd22d6ae5b91648b6c1a606ab4cdf30/hack/release/build/push-node.sh
# Example usage: `tools/build-kind-image.sh ~/go/src/k8s.io/kubernetes gcr.io/istio-testing/kind-node:v1.23.4`

set -uex

kdir="${1:?Kubernetes directory}"
registry="${2:?registry}"

ARCHES="${ARCHES:-amd64 arm64}"
IFS=" " read -r -a __arches__ <<< "$ARCHES"

images=()
for arch in "${__arches__[@]}"; do
    image="${registry}-${arch}"
    kind build node-image --image="${image}" --arch="${arch}" --kube-root "${kdir}"
    images+=("${image}")
done

# combine to manifest list tagged with kubernetes version
export DOCKER_CLI_EXPERIMENTAL=enabled
# images must be pushed to be referenced by docker manifest
# we push only after all builds have succeeded
for image in "${images[@]}"; do
    docker push "${image}"
done
docker manifest rm "${registry}" || true
docker manifest create "${registry}" "${images[@]}"
docker manifest push "${registry}"
