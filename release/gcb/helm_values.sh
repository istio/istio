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

set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"

SCRIPTPATH=$( cd "$(dirname "$0")" ; pwd -P )
# shellcheck source=release/gcb/gcb_lib.sh
source "${SCRIPTPATH}/gcb_lib.sh"

# This script updates helm config files and add helm charts to the release tarballs.
# switch to the root of the istio repo
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

mkdir -p modification-tmp
cd    modification-tmp || exit 2
ls -l
pwd

fix_values_yaml "${CB_VERSION}" "${CB_DOCKER_HUB}"

#filename | sha256 hash
#-------- | -----------
#[kubernetes.tar.gz](https://dl.k8s.io/v1.10.6/kubernetes.tar.gz) | `dbb1e757ea8fe5e82796db8604d3fc61f8b79ba189af8e3b618d86fcae93dfd0`
