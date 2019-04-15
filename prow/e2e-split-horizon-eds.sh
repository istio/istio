#!/bin/bash

# Copyright 2019 Istio Authors
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

export RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
export OWNER="istio-multicluster-split-horizon-e2e"

export SETUP_CLUSTERREG="True"
CLUSTERREG_DIR="${CLUSTERREG_DIR:-$(mktemp -d /tmp/clusterregXXX)}"
export CLUSTERREG_DIR

./prow/e2e-suite.sh --timeout 15 --cluster_registry_dir="$CLUSTERREG_DIR" --single_test e2e_multicluster_split_horizon "$@"
