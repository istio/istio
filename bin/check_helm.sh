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

helm lint ./install/kubernetes/helm/istio
helm template install/kubernetes/helm/istio --name istio --namespace istio-system -x templates/configmap.yaml | grep -q " $" && echo "templates/configmap.yaml has trailing spaces" && exit 1
helm template install/kubernetes/helm/istio --name istio --namespace istio-system -x templates/sidecar-injector-configmap.yaml | grep -q " $" && exit 1

exit 0
