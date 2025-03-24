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

set -ex

fail() {
  echo "$@" 1>&2
  exit 1
}

API_TMP="$(mktemp -d -u)"

trap 'rm -rf "${API_TMP}"' EXIT

SCRIPTPATH="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
ROOTDIR=$(dirname "${SCRIPTPATH}")
cd "${ROOTDIR}"

REPO="github.com/istio/api"
# using the pseudo version we have in go.mod file. e.g. v.0.0.0-<timestamp>-<SHA>
# first check if there's a replace: e.g. replace istio.io/api => github.com/USER/istioapi v0.0.0-<timestamp>-<SHA>
SHA=$(grep "istio.io/api" go.mod | grep "^replace" | awk -F "-" '{print $NF}')
if [ -n "${SHA}" ]; then
  REPO=$(grep "istio.io/api" go.mod | grep "^replace" | awk '{print $4}')
else
  SHA=$(grep "istio.io/api" go.mod | head -n1 | awk '{ print $2 }')
  if [[ ${SHA} == *"-"* && ! ${SHA} =~ -rc.[0-9]$ && ! ${SHA} =~ -beta.[0-9]$ && ! ${SHA} =~ -alpha.[0-9]$ ]]; then
    # not an official release or release candidate, so get the commit sha
    SHA=$(echo "${SHA}" | awk -F '-' '{ print $NF }')
  fi
fi

if [ -z "${SHA}" ]; then
  fail "Unable to retrieve the commit SHA of istio/api from go.mod file. Not updating the CRD file. Please make sure istio/api exists in the Go module.";
fi

git clone --filter=tree:0 "https://${REPO}" "${API_TMP}" && cd "${API_TMP}"
git checkout "${SHA}"
if [ ! -f "${API_TMP}/kubernetes/customresourcedefinitions.gen.yaml" ]; then
  echo "Generated Custom Resource Definitions file does not exist in the commit SHA ${SHA}. Not updating the CRD file."
  exit
fi
rm -f "${ROOTDIR}/manifests/charts/base/files/crd-all.gen.yaml"
cp "${API_TMP}/kubernetes/customresourcedefinitions.gen.yaml" "${ROOTDIR}/manifests/charts/base/files/crd-all.gen.yaml"
cp "${API_TMP}"/tests/testdata/* "${ROOTDIR}/pkg/config/validation/testdata/crds"

cd "${ROOTDIR}"

# TODO(liorlieberman): support gateway-api-inference-extension crds?
GATEWAY_VERSION=$(grep "gateway-api\s" go.mod | awk '{ print $2 }')
if [[ ${GATEWAY_VERSION} == *"-"* && ! ${GATEWAY_VERSION} =~ -rc.?[0-9]$ ]]; then
  # not an official release or release candidate, so get the commit sha
  SHORT_SHA=$(echo "${GATEWAY_VERSION}" | awk -F '-' '{ print $NF }')
  GATEWAY_VERSION=$(curl -s -L -H "Accept: application/vnd.github+json" -H "X-GitHub-Api-Version: 2022-11-28" "https://api.github.com/repos/kubernetes-sigs/gateway-api/commits/${SHORT_SHA}" | jq -r .sha)
fi
if [ -z "${GATEWAY_VERSION}" ]; then
  fail "Unable to retrieve the gateway-api version from go.mod file. Not updating the CRD file.";
fi

echo "# Generated with \`kubectl kustomize \"https://github.com/kubernetes-sigs/gateway-api/config/crd/experimental?ref=${GATEWAY_VERSION}\"\`" > "${API_TMP}/gateway-api-crd.yaml"
if ! kubectl kustomize "github.com/kubernetes-sigs/gateway-api/config/crd/experimental?ref=${GATEWAY_VERSION}" >> "${API_TMP}/gateway-api-crd.yaml"; then
  fail "Unable to generate the CRDs for ${GATEWAY_VERSION}. Not updating the CRD file.";
fi

rm -f "${ROOTDIR}/tests/integration/pilot/testdata/gateway-api-crd.yaml"
cp "${API_TMP}/gateway-api-crd.yaml" "${ROOTDIR}/tests/integration/pilot/testdata/gateway-api-crd.yaml"
