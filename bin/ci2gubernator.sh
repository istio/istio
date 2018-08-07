#!/bin/bash

# Exit immediately for non zero status
set -e

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname ${SCRIPTPATH})"

REQUIRED_ENVS=(
	GCS_BUCKET_TOKEN
	CIRCLE_SHA1
	CIRCLE_PROJECT_USERNAME
	CIRCLE_PROJECT_REPONAME
	CIRCLE_JOB
	CIRCLE_BUILD_NUM
)

for env in "${REQUIRED_ENVS[@]}"; do
	if eval [ -z \$${env} ]; then
		echo "${env} not defined"
		exit 0
	fi
done

# The GCP service account for circle ci is only authorized to edit gs://istio-circleci bucket
TMP_SA_JSON=$(mktemp /tmp/XXXXX.json)
ENCRYPTED_SA_JSON="${ROOTDIR}/.circleci/accounts/istio-circle-ci.gcp.serviceaccount"
openssl aes-256-cbc -d -in "${ENCRYPTED_SA_JSON}" -out "${TMP_SA_JSON}" -k "${GCS_BUCKET_TOKEN}" -md sha256

go get -u istio.io/test-infra/toolbox/ci2gubernator

ARGS=(
	"--service_account=${TMP_SA_JSON}" \
	"--sha=${CIRCLE_BRANCH}/${CIRCLE_SHA1}" \
	"--org=${CIRCLE_PROJECT_USERNAME}" \
	"--repo=${CIRCLE_PROJECT_REPONAME}" \
	"--job=${CIRCLE_JOB}" \
	"--build_number=${CIRCLE_BUILD_NUM}" \
	"--pr_number=${CIRCLE_PR_NUMBER:-0}"
)

if [ -n "$CIRCLE_PULL_REQUEST" ]; then
	ARGS+=("--stage=presubmit")
fi

/go/bin/ci2gubernator "${@}" "${ARGS[@]}" || true
