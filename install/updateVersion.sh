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

# This file is temporary compatibility between old update version
# and helm template based generation
set -e

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/.."

cd ${ROOT}

# just run the old version
${ROOT}/install/updateVersion_orig.sh "$*"


function gen_file() {
    fl=$1
    cp -f install/kubernetes/$fl ${ISTIO_OUT:-.}/orig_$fl
    make $1
}


for target in istio.yaml istio-auth.yaml istio-one-namespace.yaml istio-one-namespace-auth.yaml;do
    gen_file $target
done


# run something like
# helm template install/kubernetes/helm/istio -x templates/namespace.yaml
