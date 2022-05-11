#!/usr/bin/env bash

# Copyright 2020 Istio Authors
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

WD=$(dirname "$0")
# shellcheck disable=SC2164
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

pushd "${ROOT}" || exit 1


ENVOY_SHA="${ENVOY_SHA:-b50a30ee07d19eca45154c4bc093291635bec48b}"
ENVOY_DIR="out/envoy-${ENVOY_SHA}"
ENVOY_PATH="${ENVOY_PATH:-${ENVOY_DIR?}/usr/local/bin/envoy}"
if [ -n "${SKIP_COPY_ENVOY}" ]; then
  echo "Using whatever uproxy already exists in /out"
elif [ -f "${ENVOY_PATH}" ]; then
  echo "${ENVOY_PATH} already exists, skip downloading"
else
  echo "Fetching envoy at ${ENVOY_SHA}"
  mkdir -p "${ENVOY_DIR}"
  gsutil cp "gs://solo-istio-build/proxy/envoy-alpha-${ENVOY_SHA}.tar.gz" "${ENVOY_DIR}/envoy.tar.gz"
  pushd "${ENVOY_DIR}" || exit 1
  tar -xvf envoy.tar.gz
  popd || exit 1
fi

if [ -z "${SKIP_COPY_ENVOY}" ]; then
  echo "Copying envoy from ${ENVOY_PATH} to out/uproxy. Use out/uproxy in ISTIO_ENVOY_LOCAL."
  cp "${ENVOY_PATH}" "out/uproxy"
  cp "out/uproxy" "out/linux_amd64/envoy"
  cp "out/uproxy" "out/linux_amd64/release/envoy"
fi

popd || exit 1
