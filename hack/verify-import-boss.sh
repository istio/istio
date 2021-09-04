#!/usr/bin/env bash

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

# This file is copy form kubernetes.

# This script checks import restrictions. The script looks for a file called
# `.import-restrictions` in each directory, then all imports of the package are
# checked against each "rule" in the file.
# Usage: `hack/verify-import-boss.sh`.

set -o errexit
set -o nounset
set -o pipefail

ISTIO_ROOT=$(dirname "${BASH_SOURCE[0]}")/..
source "${ISTIO_ROOT}/hack/lib/init.sh"

ISTIO::golang::setup_env

go install istio.io/istio/tools/import-boss

packages=(
  "istio.io/istio/operator/..."
  "istio.io/istio/istioctl/..."
  "istio.io/istio/pilot/..."
)

$(ISTIO::util::find-binary "import-boss") --include-test-files=false --verify-only --input-dirs "$(IFS=, ; echo "${packages[*]}")"
