#!/bin/bash

# Copyright 2018 Istio Authors
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

# Exit immediately for non zero status
set -e

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname "${SCRIPTPATH}")"

# Set GOPATH to match the expected layout
GO_TOP=$(cd "$(dirname "$0")"/../../../..; pwd)
export GOPATH=${GOPATH:-$GO_TOP}

REQUIRED_ENVS=(
	GCS_BUCKET_TOKEN
	CIRCLE_SHA1
	CIRCLE_PROJECT_USERNAME
	CIRCLE_PROJECT_REPONAME
	CIRCLE_JOB
	CIRCLE_BUILD_NUM
)

for env in "${REQUIRED_ENVS[@]}"; do
	if eval [ -z \$"${env}" ]; then
		echo "${env} not defined"
		exit 0
	fi
done

# The GCP service account for circle ci is only authorized to edit gs://istio-circleci bucket
TMP_SA_JSON=$(mktemp /tmp/XXXXX.json)
ENCRYPTED_SA_JSON="${ROOTDIR}/.circleci/accounts/istio-circle-ci.gcp.serviceaccount"
openssl aes-256-cbc -d -in "${ENCRYPTED_SA_JSON}" -out "${TMP_SA_JSON}" -k "${GCS_BUCKET_TOKEN}" -md sha256

go get -u istio.io/test-infra/toolbox/ci2gubernator

if [[ -d "${GOPATH}/bin/ci2gubernator" ]];then
    echo "download istio.io/test-infra/toolbox/ci2gubernator failed"
    exit 1
fi

ARGS=(
	"--service_account=${TMP_SA_JSON}" \
	"--sha=${CIRCLE_BRANCH}/${CIRCLE_SHA1}" \
	"--org=${CIRCLE_PROJECT_USERNAME}" \
	"--repo=${CIRCLE_PROJECT_REPONAME}" \
	"--job=${CIRCLE_JOB}" \
	"--build_number=${CIRCLE_BUILD_NUM}" \
	"--pr_number=${CIRCLE_PR_NUMBER:-0}"
)

# CIRCLE_PR_NUMBER is set for PRs that originate from a fork.
# CIRCLE_PULL_REQUEST is set for PRs that originate from a branch.
if [ -n "$CIRCLE_PR_NUMBER" ] || [ -n "$CIRCLE_PULL_REQUEST" ]; then
	ARGS+=("--stage=presubmit")
fi

ci2gubernator=${GOPATH}/bin/ci2gubernator
$ci2gubernator "${@}" "${ARGS[@]}" || true
