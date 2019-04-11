#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

cd /workspace || exit 1
# /output is used to store release artifacts
mkdir /output

# start actual build steps
/workspace/generate_manifest.sh
/workspace/istio_checkout_code.sh

cd /workspace/go/src/istio.io/istio || exit 2
/workspace/cloud_builder.sh

cd /workspace || exit 3
/workspace/store_artifacts.sh
/workspace/rel_push_docker_build_version.sh
/workspace/helm_values.sh
/workspace/helm_charts.sh
