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
# shellcheck source=release/pipeline/gcb_build_lib.sh
source "${SCRIPTPATH}/gcb_build_lib.sh"

SUBS_FILE="$(mktemp /tmp/build.subs.gcs_release_tool_path.XXXX)"
cat << EOF > "${SUBS_FILE}"
substitutions": {
  "_CB_GCS_RELEASE_TOOLS_PATH": "${CB_GCS_RELEASE_TOOLS_PATH}"
}
EOF

# Called directly by Airflow.
function get_git_commit_cmd() {
    run_build "get_commit.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function build_template() {
    run_build "build.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function test_command() {
    run_build "release_qualification.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function modify_values_command() {
    run_build "modify_values.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function gcr_tag_success() {
    run_build "daily_success.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function release_push_github_docker_template() {
    run_build "github_publish.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function release_tag_github_template() {
    run_build "github_tag.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}"
    exit "$BUILD_FAILED"
}
