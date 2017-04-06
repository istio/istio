#!/bin/bash

# Copyright 2017 Istio Authors
#
#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License

set -o errexit
set -o nounset
set -o pipefail
set -x

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="${ROOT}/istio.VERSION"

# set env variables from the version file
source $VERSION_FILE

pushd $ROOT/kubernetes/istio-install
sed -i "s|image: .*/\(.*\):.*|image: $MANAGER_HUB/\1:$MANAGER_TAG|" istio-manager.yaml
sed -i "s|image: .*/\(.*\):.*|image: $MANAGER_HUB/\1:$MANAGER_TAG|" istio-ingress-controller.yaml
sed -i "s|image: .*|image: $MIXER|" istio-mixer.yaml
popd
