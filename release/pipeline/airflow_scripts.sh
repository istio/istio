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

function get_git_commit_cmd() {
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    git clone $MFEST_URL green-builds || exit 2

    pushd green-builds
    git checkout $BRANCH
    git checkout $MFEST_COMMIT || exit 3
    ISTIO_SHA=`grep $GITHUB_ORG/$GITHUB_REPO $MFEST_FILE | cut -f 6 -d \\"` || exit 4
    API_SHA=`  grep $GITHUB_ORG/api          $MFEST_FILE | cut -f 6 -d \\"` || exit 5
    PROXY_SHA=`grep $GITHUB_ORG/proxy        $MFEST_FILE | cut -f 6 -d \\"` || exit 6
    if [ -z ${ISTIO_SHA} ] || [ -z ${API_SHA} ] || [ -z ${PROXY_SHA} ]; then
      echo "ISTIO_SHA:$ISTIO_SHA API_SHA:$API_SHA PROXY_SHA:$PROXY_SHA some shas not found"
      exit 7
    fi
    popd #green-builds

    git clone $ISTIO_REPO istio-code -b $BRANCH
    pushd istio-code/release
    ISTIO_HEAD_SHA=`git rev-parse HEAD`
    git checkout ${ISTIO_SHA} || exit 8

    TS_SHA=` git show -s --format=%ct ${ISTIO_SHA}`
    TS_HEAD=`git show -s --format=%ct ${ISTIO_HEAD_SHA}`
    DIFF_SEC=$((TS_HEAD - TS_SHA))
    DIFF_DAYS=$(($DIFF_SEC/86400))
    if [ "$CHECK_GREEN_SHA_AGE" = "true" ] && [ "$DIFF_DAYS" -gt "2" ]; then
       echo ERROR: ${ISTIO_SHA} is $DIFF_DAYS days older than head of branch $BRANCH
       exit 9
    fi
    popd #istio-code/release

    if [ "$VERIFY_CONSISTENCY" = "true" ]; then
      PROXY_REPO=`dirname $ISTIO_REPO`/proxy
      echo $PROXY_REPO
      git clone $PROXY_REPO proxy-code -b $BRANCH
      pushd proxy-code
      PROXY_HEAD_SHA=`git rev-parse HEAD`
      PROXY_HEAD_API_SHA=`grep ISTIO_API istio.deps  -A 4 | grep lastStableSHA | cut -f 4 -d '"'`
      popd
      if [ "$PROXY_HEAD_SHA" != "$PROXY_SHA" ]; then
        echo "inconsistent shas     PROXY_HEAD_SHA     $PROXY_HEAD_SHA != $PROXY_SHA PROXY_SHA" 1>&2
        exit 10
      fi
      if [ "$PROXY_HEAD_API_SHA" != "$API_SHA" ]; then
        echo "inconsistent shas PROXY_HEAD_API_SHA $PROXY_HEAD_API_SHA != $API_SHA   API_SHA"   1>&2
        exit 11
      fi
      if [ "$ISTIO_HEAD_SHA" != "$ISTIO_SHA" ]; then
        echo "inconsistent shas     ISTIO_HEAD_SHA     $ISTIO_HEAD_SHA != $ISTIO_SHA ISTIO_SHA" 1>&2
        exit 12
      fi
    fi

    pushd istio-code/release
    gsutil cp *.sh   gs://$GCS_RELEASE_TOOLS_PATH/data/release/
    gsutil cp *.json gs://$GCS_RELEASE_TOOLS_PATH/data/release/
    cp airflow_scripts.sh /home/airflow/gcs/data/airflow_scripts.sh
    popd #istio-code/release

    pushd green-builds
    git rev-parse HEAD
}

#  get_git_commit = BashOperator(
#      task_id='get_git_commit',
#      bash_command=get_git_commit_cmd,
#      xcom_push=True,
#      dag=common_dag)
#  tasks['get_git_commit'] = get_git_commit

function build_template() {
    # TODO: Merge these json/script changes into istio/istio master and change these path back.
    # Currently we did changes and push those to a test-version folder manually
    gsutil cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/*.json .
    gsutil cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/*.sh .
    chmod u+x *

    ./start_gcb_build.sh -w -p $PROJECT_ID -r $GCR_STAGING_DEST -s $GCS_BUILD_PATH \
    -v "$VERSION" -u "$MFEST_URL" -t "{{ m_commit }}" -m "$MFEST_FILE" -a $SVC_ACCT
  # NOTE: if you add commands to build_template after start_gcb_build.sh then take care to preserve its return value
}

#  build = BashOperator(
#      task_id='run_cloud_builder', bash_command=build_template, dag=common_dag)
#  tasks['run_cloud_builder'] = build

function test_command() {
    cp /home/airflow/gcs/data/githubctl ./githubctl
    chmod u+x ./githubctl
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    ls -l    ./githubctl
    ./githubctl \
    --token_file="$TOKEN_FILE" \
    --op=dailyRelQual \
    --hub=gcr.io/$GCR_STAGING_DEST \
    --gcs_path="$GCS_BUILD_PATH" \
    --tag="$VERSION" \
    --base_branch="$BRANCH"
}

#  run_release_qualification_tests = BashOperator(
#      task_id='run_release_qualification_tests',
#      bash_command=test_command,
#      retries=0,
#      dag=common_dag)
#  tasks['run_release_qualification_tests'] = run_release_qualification_tests

function modify_values_command() {
    gsutil cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/modify_values.sh .
    chmod u+x modify_values.sh
    echo "PIPELINE TYPE is $PIPELINE_TYPE"
    if [ "$PIPELINE_TYPE" = "daily" ]; then
        hub="gcr.io/$GCR_STAGING_DEST"
    elif [ "$PIPELINE_TYPE" = "monthly" ]; then
        hub="docker.io/istio"
    fi
    ./modify_values.sh -h "${hub}" -t $VERSION -p gs://$GCS_BUILD_BUCKET/$GCS_STAGING_PATH -v $VERSION
}

#  modify_values = BashOperator(
#    task_id='modify_values_helm', bash_command=modify_values_command, dag=common_dag)
#  tasks['modify_values_helm'] = modify_values


function gcr_tag_success() {
#KPTD {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}

set -x
pwd; ls

gsutil ls gs://$GCS_FULL_STAGING_PATH/docker/           > docker_tars.txt
cat docker_tars.txt |   grep -Eo "docker\/(([a-z]|[0-9]|-|_)*).tar.gz" | \
                        sed -E "s/docker\/(([a-z]|[0-9]|-|_)*).tar.gz/\1/g" > docker_images.txt

  gcloud auth configure-docker  -q
  cat docker_images.txt | \
  while read -r docker_image;do
    gcloud container images add-tag \
    "gcr.io/$GCR_STAGING_DEST/${docker_image}:$VERSION" \
    "gcr.io/$GCR_STAGING_DEST/${docker_image}:$BRANCH-latest-daily" --quiet;
    #pull_source="gcr.io/$GCR_STAGING_DEST/${docker_image}:$VERSION"
    #push_dest="  gcr.io/$GCR_STAGING_DEST/${docker_image}:latest_$BRANCH";
    #docker pull $pull_source
    #docker tag  $pull_source $push_dest
    #docker push $push_dest
  done

cat docker_tars.txt docker_images.txt
rm  docker_tars.txt docker_images.txt
}

#  tag_daily_grc = BashOperator(
#      task_id='tag_daily_gcr',
#      bash_command=gcr_tag_success,
#      dag=dag)

function release_push_github_docker_template() {
#KPTD {% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
#KPTD {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}

gsutil cp gs://$GCS_RELEASE_TOOLS_PATH/data/release/*.json .
gsutil cp gs://$GCS_RELEASE_TOOLS_PATH/data/release/*.sh .
chmod u+x *
./start_gcb_publish.sh \
-p "$RELEASE_PROJECT_ID" -a "$SVC_ACCT"  \
-v "$VERSION" -s "$GCS_FULL_STAGING_PATH" \
-b "$GCS_MONTHLY_RELEASE_PATH" -r "$GCR_RELEASE_DEST" \
-g "$GCS_GITHUB_PATH" -u "$MFEST_URL" \
-t "{{ m_commit }}" -m "$MFEST_FILE" \
-h "$GITHUB_ORG" -i "$GITHUB_REPO" \
-d "$DOCKER_HUB}}" -w
}

#  github_and_docker_release = BashOperator(
#    task_id='github_and_docker_release',
#    bash_command=release_push_github_docker_template,
#    dag=dag)
#  tasks['github_and_docker_release'] = github_and_docker_release

function release_tag_github_template() {
#KPTD {% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
#KPTD {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
gsutil cp gs://$GCS_RELEASE_TOOLS_PATH/data/release/*.json .
gsutil cp gs://$GCS_RELEASE_TOOLS_PATH/data/release/*.sh .
chmod u+x *
./start_gcb_tag.sh \
-p "$RELEASE_PROJECT_ID" \
-h "$GITHUB_ORG" -a "$SVC_ACCT"  \
-v "$VERSION"   -e "istio_releaser_bot@example.com" \
-n "IstioReleaserBot" -s "$GCS_FULL_STAGING_PATH" \
-g "$GCS_GITHUB_PATH" -u "$MFEST_URL" \
-t "{{ m_commit }}" -m "$MFEST_FILE" -w
}

#  github_tag_repos = BashOperator(
#    task_id='github_tag_repos',
#    bash_command=release_tag_github_template,
#    dag=dag)
#  tasks['github_tag_repos'] = github_tag_repos

