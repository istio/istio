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

function get_git_commit_cmd() {
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"

    ./start_gcb_get_commit.sh
}

function build_template() {
    ./start_gcb_build.sh -w -r "$GCR_STAGING_DEST" -s "$GCS_BUILD_PATH" -v "$VERSION"
  # NOTE: if you add commands to build_template after start_gcb_build.sh then take care to preserve its return value
}

function test_command() {
    cp /home/airflow/gcs/data/githubctl ./githubctl
    chmod u+x ./githubctl
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    ls -l    ./githubctl
    ./githubctl \
    --token_file="$TOKEN_FILE" \
    --op=dailyRelQual \
    --hub="gcr.io/$GCR_STAGING_DEST" \
    --gcs_path="$GCS_BUILD_PATH" \
    --tag="$VERSION" \
    --base_branch="$BRANCH"
}

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

function gcr_tag_success() {
  pwd; ls

  gsutil ls "gs://$GCS_FULL_STAGING_PATH/docker/"             > docker_tars.txt
  grep -Eo "docker\\/(([a-z]|[0-9]|-|_)*).tar.gz"               docker_tars.txt \
      | sed -E "s/docker\\/(([a-z]|[0-9]|-|_)*).tar.gz/\\1/g" > docker_images.txt

  gcloud auth configure-docker  -q
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

function release_push_github_docker_template() {
  ./start_gcb_publish.sh \
    -b "$GCS_MONTHLY_RELEASE_PATH" \
    -c "$GCS_BUILD_PATH" \
    -d "$DOCKER_HUB" \
    -g "$GCS_GITHUB_PATH" \
    -h "$GITHUB_ORG" \
    -i "$GITHUB_REPO" \
    -r "$GCR_RELEASE_DEST" \
    -s "$GCS_FULL_STAGING_PATH" \
    -v "$VERSION" \
    -w
}

function release_tag_github_template() {
  ./start_gcb_tag.sh \
    -c "$GCS_BUILD_PATH" \
    -e "istio_releaser_bot@example.com" \
    -g "$GCS_GITHUB_PATH" \
    -h "$GITHUB_ORG" \
    -n "IstioReleaserBot" \
    -s "$GCS_FULL_STAGING_PATH" \
    -v "$VERSION" \
    -w
}
