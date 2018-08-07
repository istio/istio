#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# set this to the new multicluster-e2e type
export RESOURCE_TYPE="${RESOURCE_TYPE:-gke-e2e-test}"
export OWNER="istio-pilot-multicluster-e2e"

export SETUP_CLUSTERREG="True"
CLUSTERREG_DIR="${CLUSTERREG_DIR:-$(mktemp -d /tmp/clusterregXXX)}"
export CLUSTERREG_DIR

#echo 'Running pilot multi-cluster e2e tests (v1alpha1, noauth)'
./prow/e2e-suite.sh --timeout 35 --cluster_registry_dir="$CLUSTERREG_DIR" --single_test e2e_pilotv2_v1alpha3 "$@"
