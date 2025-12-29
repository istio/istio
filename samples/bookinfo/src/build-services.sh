#!/bin/bash
#
# Copyright Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at
#
#       http://www.apache.org/licenses/LICENSE-2.0
#
#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -ox errexit

# Get to the root directory of the repo...
SCRIPTDIR=$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )
cd "$SCRIPTDIR/../../.."

h="${BOOKINFO_HUB:?BOOKINFO_HUB must be set}"
t="${BOOKINFO_TAG:?BOOKINFO_TAG must be set}"
if [[ ("${h}" == "istio" || "${h}" == "docker.io/istio") && -z "$CI" && "$*" =~ "--push" ]]; then
  echo "Can only push to prod registry in CI"
  exit 1
fi

if [[ "${BOOKINFO_LATEST}" == "true" ]]; then
  BOOKINFO_TAG="${BOOKINFO_TAG},latest"
fi

# Pass input args to the command. This allows using --push, --load, etc
env TAGS="${BOOKINFO_TAG}" HUB="${BOOKINFO_HUB}" \
  docker buildx bake -f samples/bookinfo/src/docker-bake.hcl "$@"

if [[ "${BOOKINFO_UPDATE}" == "true" ]]; then
# Update image references in the yaml files
  find ./samples/bookinfo/platform -name "*bookinfo*.yaml" -exec sed -i.bak "s#image:.*\\(\\/examples-bookinfo-.*\\):.*#image: ${h//\//\\/}\\1:$t#g" {} +
fi

