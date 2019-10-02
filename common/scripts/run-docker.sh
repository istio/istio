#!/bin/bash
#
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

set -ex

istio_dir=$(git rev-parse --show-toplevel)
repo_name=$(basename $istio_dir)
target_out=$HOME/istio_out/$repo_name

image="gcr.io/istio-testing/build-tools:latest"

docker pull $image

docker run -it --rm -u $(id -u) \
    -u root \
    --cap-add=NET_ADMIN \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -v /etc/passwd:/etc/passwd:ro \
    $CONTAINER_OPTIONS \
    -e WHAT=$WHAT \
    --mount type=bind,source="$istio_dir",destination="/work" \
    --mount type=bind,source="$target_out",destination="/targetout" \
    --mount type=volume,source=home,destination="/home" \
    -w /work $image $@

