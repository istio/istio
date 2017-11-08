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
WD=$(dirname $0)
WD=$(cd $WD; pwd)
ROOT=$(dirname $(dirname $WD))

source ${ROOT}/prow/istio-common.sh
# Build
${ROOT}/bin/init.sh

echo "=== Code Coverage ==="
UPLOAD_TOKEN=@/etc/codecov/istio.token ${ROOT}/bin/codecov.sh | tee codecov.report
if [ "${CI:-}" == "bootstrap" ]; then
    bazel build @com_github_istio_test_infra//toolbox/pkg_check
    BUILD_ID="PROW-${BUILD_NUMBER}" JOB_NAME="istio/presubmit" ${ROOT}/bazel-bin/external/com_github_istio_test_infra/toolbox/pkg_check/pkg_check
else
    echo "Not in bootstrap environment, skipping code coverage publishing"
fi
