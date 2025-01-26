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

set -eux

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

# shellcheck source=prow/lib.sh
source "${ROOT}/prow/lib.sh"
setup_and_export_git_sha

# Define paths for reports
REPORT_COVERAGE="${REPORT_COVERAGE:-${ARTIFACTS}/coverage.out}"

mkdir -p "$(dirname "${REPORT_COVERAGE}")"
# Run the Go coverage test
go test ./... -coverprofile="${REPORT_COVERAGE}" || true
if ! command -v overcover &>/dev/null; then
  echo "Overcover is not installed. Installing..."
  go install github.com/klmitch/overcover@latest
fi
# Run Overcover to validate coverage threshold
overcover --coverprofile="${REPORT_COVERAGE}" ./cni/...
echo "Coverage report generated: ${REPORT_COVERAGE}"
