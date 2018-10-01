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

# Helper. Not called directly by Airflow.
function sub_str_for_variable() {
   local CUR_VAR
   local subs_str

   CUR_VAR="${1}"
   subs_str="\"_$CUR_VAR\": \"${!CUR_VAR}\""
   echo -n "${subs_str}"
}

# Helper. Not called directly by Airflow.
# converts the call: create_subs_file "BRANCH" "COMMIT"
# writes the following into SUBS_FILE (which is set by function)
# and creates the file with content
#  "substitutions": {
#    "_BRANCH": "${BRANCH}",
#    "_COMMIT": "${COMMIT}"
#  }
function create_subs_file() {
   local CUR_VAR
   local subs_str
   # SUBS_FILE is not local and is output to caller
   SUBS_FILE="$(mktemp /tmp/build.subs.XXXX)"

   echo '"substitutions": {' > "${SUBS_FILE}"
   # shellcheck disable=SC2034
   for i in $(seq 2 1 $#) # print with , for n-1
   do
     CUR_VAR="${1}"
     shift
     subs_str=$(sub_str_for_variable "${CUR_VAR}")
     echo "${subs_str},"    >> "${SUBS_FILE}"
   done
   CUR_VAR="${1}"
   subs_str=$(sub_str_for_variable "${CUR_VAR}")
   # last subs line has no ,
   echo "${subs_str}"       >> "${SUBS_FILE}"
   echo '}'                 >> "${SUBS_FILE}"
}

# Called directly by Airflow.
function get_git_commit_cmd() {
    local WAIT_FOR_RESULT
    WAIT_FOR_RESULT="true"

    # uses the environment variables from next line + $PROJECT_ID $SVC_ACCT
    create_subs_file "BRANCH" "CHECK_GREEN_SHA_AGE" "COMMIT" "GCS_RELEASE_TOOLS_PATH" "VERIFY_CONSISTENCY"
    cat "${SUBS_FILE}"

    run_build "cloud_get_commit.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${WAIT_FOR_RESULT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function build_template() {
    local WAIT_FOR_RESULT
    WAIT_FOR_RESULT="true"

    # shellcheck disable=SC2034
    GCR_PATH="${GCR_STAGING_DEST}"
    GCS_PATH="${GCS_BUILD_PATH}"
    VER_STRING="${VERSION}"
    create_subs_file "BRANCH" "GCR_PATH" "GCS_PATH" "GCS_RELEASE_TOOLS_PATH" "VER_STRING"
    cat "${SUBS_FILE}"

    run_build "cloud_build.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${WAIT_FOR_RESULT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function test_command() {
    local WAIT_FOR_RESULT
    WAIT_FOR_RESULT="true"

    DOCKER_HUB="gcr.io/$GCR_STAGING_DEST"
    create_subs_file "BRANCH" "DOCKER_HUB" "GCS_BUILD_PATH" "GCS_RELEASE_TOOLS_PATH" "VERSION"
    cat "${SUBS_FILE}"

    run_build "cloud_test.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${WAIT_FOR_RESULT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function modify_values_command() {
    # TODO: Merge these changes into istio/istio master and stop using this task
    gsutil -q cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/modify_values.sh .
    chmod u+x modify_values.sh

    echo "PIPELINE TYPE is $PIPELINE_TYPE"
    if [ "$PIPELINE_TYPE" = "daily" ]; then
        hub="gcr.io/$GCR_STAGING_DEST"
    elif [ "$PIPELINE_TYPE" = "monthly" ]; then
        hub="docker.io/istio"
    fi
    ./modify_values.sh -h "${hub}" -t "$VERSION" -p "gs://$GCS_BUILD_BUCKET/$GCS_STAGING_PATH" -v "$VERSION"
}

# Called directly by Airflow.
function gcr_tag_success() {
  pwd; ls

  gsutil ls "gs://$GCS_FULL_STAGING_PATH/docker/"             > docker_tars.txt
  grep -Eo "docker\\/(([a-z]|[0-9]|-|_)*).tar.gz"               docker_tars.txt \
      | sed -E "s/docker\\/(([a-z]|[0-9]|-|_)*).tar.gz/\\1/g" > docker_images.txt

  #gcloud auth configure-docker  -q
  while read -r docker_image; do
    gcloud container images add-tag \
    "gcr.io/$GCR_STAGING_DEST/${docker_image}:$VERSION" \
    "gcr.io/$GCR_STAGING_DEST/${docker_image}:$BRANCH-latest-daily" --quiet;
    #pull_source="gcr.io/$GCR_STAGING_DEST/${docker_image}:$VERSION"
    #push_dest="  gcr.io/$GCR_STAGING_DEST/${docker_image}:latest_$BRANCH";
    #docker pull $pull_source
    #docker tag  $pull_source $push_dest
    #docker push $push_dest
  done < docker_images.txt

  cat docker_tars.txt docker_images.txt
  rm  docker_tars.txt docker_images.txt
}

# Called directly by Airflow.
function release_push_github_docker_template() {
    local WAIT_FOR_RESULT
    WAIT_FOR_RESULT="true"

    # uses the environment variables from list below + $PROJECT_ID $SVC_ACCT
    #BRANCH
    # shellcheck disable=SC2034
    DOCKER_DST="$DOCKER_HUB"
    # shellcheck disable=SC2034
    GCR_DST="${GCR_RELEASE_DEST}"
    # shellcheck disable=SC2034
    GCS_DST="${GCS_MONTHLY_RELEASE_PATH}"
    GCS_PATH="${GCS_BUILD_PATH}"
    #GCS_RELEASE_TOOLS_PATH
    GCS_SECRET="${GCS_GITHUB_PATH}"
    GCS_SOURCE="${GCS_FULL_STAGING_PATH}"
    ORG="${GITHUB_ORG}"
    # shellcheck disable=SC2034
    REPO="${GITHUB_REPO}"
    VER_STRING="${VERSION}"
    create_subs_file "BRANCH" "DOCKER_DST" "GCR_DST" "GCS_DST" "GCS_PATH" "GCS_RELEASE_TOOLS_PATH" "GCS_SECRET" "GCS_SOURCE" "ORG" "REPO" "VER_STRING"
    cat "${SUBS_FILE}"

    run_build "cloud_publish.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${WAIT_FOR_RESULT}"
    exit "${BUILD_FAILED}"
}

# Called directly by Airflow.
function release_tag_github_template() {
    local WAIT_FOR_RESULT
    WAIT_FOR_RESULT="true"

    # uses the environment variables from list below + $PROJECT_ID $SVC_ACCT
    #BRANCH
    # shellcheck disable=SC2034
    GCS_PATH="${GCS_BUILD_PATH}"
    #GCS_RELEASE_TOOLS_PATH
    # shellcheck disable=SC2034
    GCS_SECRET="${GCS_GITHUB_PATH}"
    # shellcheck disable=SC2034
    GCS_SOURCE="${GCS_FULL_STAGING_PATH}"
    # shellcheck disable=SC2034
    ORG="${GITHUB_ORG}"
    # shellcheck disable=SC2034
    USER_EMAIL="istio_releaser_bot@example.com"
    # shellcheck disable=SC2034
    USER_NAME="IstioReleaserBot"
    # shellcheck disable=SC2034
    VER_STRING="${VERSION}"
    create_subs_file "BRANCH" "GCS_PATH" "GCS_RELEASE_TOOLS_PATH" "GCS_SECRET" "GCS_SOURCE" "ORG" "USER_EMAIL" "USER_NAME" "VER_STRING"
    cat "${SUBS_FILE}"

    run_build "cloud_tag.template.json" \
         "${SUBS_FILE}" "${PROJECT_ID}" "${SVC_ACCT}" "${WAIT_FOR_RESULT}"
    exit "$BUILD_FAILED"
}
