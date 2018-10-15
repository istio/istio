#!/bin/bash
# Airfow DAG and helpers used in one or more istio release pipeline."""
# Copyright 2018 Istio Authors. All Rights Reserved.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#   http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# shellcheck disable=SC2154

set -o nounset
set -x

SCRIPTPATH=$( pwd -P )
# shellcheck source=release/airflow/gcb_build_lib.sh
source "${SCRIPTPATH}/gcb_build_lib.sh"

# Helper function called by Airflow to create SUBS file used only in this file
function create_subs_file() {
   # SUBS_FILE is not a local variable and is used by other functions in this file
   SUBS_FILE="$(mktemp /tmp/build.subs.gcs_release_tool_path.XXXX)"
cat << EOF > "${SUBS_FILE}"
  "substitutions": {
    "_CB_GCS_RELEASE_TOOLS_PATH": "${CB_GCS_RELEASE_TOOLS_PATH}"
  }
EOF
}

# Called directly by Airflow.
function get_git_commit_cmd() {
   SUBS_GGC_FILE="$(mktemp /tmp/build.subs.branch.gcs_release_tool_path.XXXX)"
cat << EOF > "${SUBS_GGC_FILE}"
  "substitutions": {
    "_CB_BRANCH": "${CB_BRANCH}",
    "_CB_GCS_RELEASE_TOOLS_PATH": "${CB_GCS_RELEASE_TOOLS_PATH}"
  }
EOF
    run_build "cloud_get_commit.template.json" \
         "${SUBS_GGC_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function build_template() {
    create_subs_file
    run_build "cloud_build.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function test_command() {
    create_subs_file
    run_build "cloud_test.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function modify_values_command() {
    create_subs_file
    # shellcheck disable=SC2034
    GCS_PATH="gs://$GCS_BUILD_BUCKET/$GCS_STAGING_PATH"
    run_build "cloud_modify_values.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function gcr_tag_success() {
    create_subs_file
    run_build "cloud_daily_success.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function release_push_github_docker_template() {
    # uses the environment variables from list below + $PROJECT_ID $SVC_ACCT
    GCS_DST="${GCS_MONTHLY_RELEASE_PATH}"
    GCS_SECRET="${GCS_GITHUB_PATH}"
    GCS_SOURCE="${GCS_FULL_STAGING_PATH}"
    create_subs_file "BRANCH" "GCS_DST" "GCS_RELEASE_TOOLS_PATH" "GCS_SECRET" "GCS_SOURCE" "GITHUB_ORG" "GITHUB_REPO" "VERSION"

    run_build "cloud_publish.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function release_tag_github_template() {
    # uses the environment variables from list below + $PROJECT_ID $SVC_ACCT
    GCS_SECRET="${GCS_GITHUB_PATH}"
    GCS_SOURCE="${GCS_FULL_STAGING_PATH}"
    USER_EMAIL="istio_releaser_bot@example.com"
    USER_NAME="IstioReleaserBot"
    create_subs_file "BRANCH" "GCS_RELEASE_TOOLS_PATH" "GCS_SECRET" "GCS_SOURCE" "GITHUB_ORG" "USER_EMAIL" "USER_NAME" "VERSION"

    run_build "cloud_tag.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "$BUILD_FAILED"
}
