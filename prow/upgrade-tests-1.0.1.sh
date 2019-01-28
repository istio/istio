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

# Local run example:
# UPGRADE_TEST_LOCAL="true" HUB=gcr.io/istio-release \
#   ISTIO_REL_URL=https://storage.googleapis.com/istio-release-pipeline-data/daily-build/master-20190107-09-15 \
#   TAG=master-20190107-09-15 \
#   prow/upgrade-tests-1.0.1.sh

# Set up inputs needed by test_upgrade.sh
export SOURCE_VERSION=1.0.1
export SOURCE_RELEASE_PATH="https://github.com/istio/istio/releases/download/${SOURCE_VERSION}"
export TARGET_VERSION=${TAG}
export TARGET_RELEASE_PATH=${ISTIO_REL_URL}

# Run the corresponding test in istio source code.
./prow/upgrade-tests.sh

