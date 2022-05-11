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

HUB=${HUB:-gcr.io/solo-test-236622}
TAG=${TAG:-ambient-oss}

function pull_to_local_registry() {
  local REMOTE
  local LOCAL
  for i in pilot proxyv2 uproxy; do
    REMOTE="${HUB}/${i}:${TAG}"
    LOCAL="localhost:5000/${i}:${TAG}"
    docker pull "${REMOTE}"
    docker tag "${REMOTE}" "${LOCAL}"
    docker push "${LOCAL}"
  done
}

pull_to_local_registry

