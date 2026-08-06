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

set -e

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"

# Resolve the latest published stable agentgateway image tag. Unlike proxy/ztunnel, which are
# istio.io repos pinned to a master commit SHA, agentgateway is consumed as a released image mirror
# (cr.agentgateway.dev/agentgateway), so we take the highest vX.Y.Z tag that actually has a
# published image. Pre-release tags (e.g. -rc, -beta) are ignored.
function getLatestTag() {
  crane ls cr.agentgateway.dev/agentgateway | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | sort -V | tail -n1
}

TAG=$(getLatestTag)
if [[ -z "${TAG}" ]]; then
  echo "failed to resolve latest agentgateway image tag from cr.agentgateway.dev/agentgateway" >&2
  exit 1
fi

sed -i '/AGENTGATEWAY_IMAGE/,/lastStableSHA/ { s/"lastStableSHA":.*/"lastStableSHA": "'"${TAG}"'"/  }' istio.deps
