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

set -ex

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)
ROOT=$(dirname "$WD")

cd "${ROOT}/../../.."

# Very simple test to validate we can push the new image and it doesn't break fortio upgrade in a test cluster
# Should be a post-submit - it will build asm-canary image and push it, not yet designed for temp clusters, doesn't
# cleanup old tags.

# TODO: add more clusters and projects, with different versions of K8S and add-on
# TODO: trigger more tests in the test clusters

# The tests clusters most grant prow-gob-storage@istio-prow-build.iam.gserviceaccount.com k8s admin, and
# be prepared by installing managed Istiod once. The tests will upgrade Istiod to current post-submit version.
# Currently run as part of test - should be moved to separate suite
# The project is pre-provisioned, we're testing new image on upgrade only

export PROJECT_ID="${PROJECT_ID:-asm-cloudrun}"
export ZONE="${ZONE:-us-central1-c}"
export CLUSTER="${CLUSTER:-gvisor}"

export TAG="${TAG:-asm-canary}"
export HUB="${HUB:-gcr.io/wlhe-cr}"

# Build and push the proxy and cloudrun image
export BUILD_ALL=false
export DOCKER_TARGETS="docker.cloudrun docker.proxyv2"
make dockerx.push

# Deploy the new revision
gcloud container clusters get-credentials "${CLUSTER}" --zone "${ZONE}" --project "${PROJECT_ID}" --billing-project "${PROJECT_ID}"
kubectl delete mutatingwebhookconfiguration istiod-asmca || true
curl  --request POST  --header 'X-Server-Timeout: 600'     \
	--header "Authorization: Bearer $(shell gcloud auth print-access-token)"    \
	--header "Content-Type: application/json" \
    --data "{\"image\": \"${HUB}/cloudrun:${TAG}\"}" \
	"https://staging-meshconfig.sandbox.googleapis.com/v1alpha1/projects/${PROJECT_ID}/locations/${ZONE}/clusters/${CLUSTER}:runIstiod"

# Update fortio deployment to pick up the changes
kubectl -n fortio-asmca rollout restart deployment
kubectl wait deployments fortio -n fortio-asmca --for=condition=available --timeout=30s
