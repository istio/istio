#!/usr/bin/env bash

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
# set -x

SCRIPTPATH="$(cd "$(dirname "$0")" ; pwd -P)"
ROOTDIR="$(dirname ${SCRIPTPATH})"

if [ -z "$GCS_BUCKET_TOKEN" ]; then
	echo "GCS_BUCKET_TOKEN unavailible"
	exit 0
fi

if [ -z "$CIRCLE_PR_NUMBER" ] || [ -z "$CIRCLE_SHA1" ] || [ -z "$CIRCLE_PROJECT_USERNAME" ] \
	|| [ -z "$CIRCLE_PROJECT_REPONAME" ] || [ -z "$CIRCLE_JOB" ] || [ -z "$CIRCLE_BUILD_NUM" ]; then
	echo "Circle CI envs incomplete"
	exit 0
fi

# The GCP service account for circle ci is only authorized to edit gs://istio-circleci bucket
TMP_SA_JSON=$(mktemp /tmp/XXXXX.json)
ENCRYPTED_SA_JSON="${ROOTDIR}/.circleci/accounts/istio-circle-ci.gcp.serviceaccount"
openssl aes-256-cbc -d -in "${ENCRYPTED_SA_JSON}" -out "${TMP_SA_JSON}" -k "${GCS_BUCKET_TOKEN}" -md sha256

go get -u istio.io/test-infra/toolbox/ci2gubernator

/go/bin/ci2gubernator ${@} \
--service_account="${TMP_SA_JSON}" \
--sha=${CIRCLE_SHA1} \
--org=${CIRCLE_PROJECT_USERNAME} \
--repo=${CIRCLE_PROJECT_REPONAME} \
--job=${CIRCLE_JOB} \
--build_number=${CIRCLE_BUILD_NUM} \
--pr_number=${CIRCLE_PR_NUMBER}
