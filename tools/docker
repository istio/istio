#!/bin/bash

# Copyright Istio Authors. All Rights Reserved.
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

# Setup env vars
source ./common/scripts/setup_env.sh

set -eu

# Speed up builds when using recent CPUs
export GGCR_EXPERIMENT_SHA256_SIMD=1

if [[ -f ${LOCAL_OUT}/docker-builder ]]; then
  CURRENT_BUILD="$(./common/scripts/report_build_info.sh | grep buildGitRevision | cut -d= -f2)"
  PREBUILT="$(${LOCAL_OUT}/docker-builder --version)"
  CODE_CHG="$(git diff HEAD -- ./tools/docker-builder)"
  if [[ "${CURRENT_BUILD}" != "${PREBUILT}" ]] || [[ -n "${CODE_CHG}" ]]; then
    GOOS=${LOCAL_GO_OS} GOARCH=${LOCAL_GO_ARCH} ./common/scripts/gobuild.sh ${LOCAL_OUT}/docker-builder ./tools/docker-builder
  fi
else
  GOOS=${LOCAL_GO_OS} GOARCH=${LOCAL_GO_ARCH} ./common/scripts/gobuild.sh ${LOCAL_OUT}/docker-builder ./tools/docker-builder
fi
${LOCAL_OUT}/docker-builder "$@"
