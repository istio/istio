"""Airfow DAG and helpers used in one or more istio release pipeline."""
"""Copyright 2017 Istio Authors. All Rights Reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

   http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
"""
import datetime
import logging
import time

from airflow import DAG
from airflow.models import Variable
from airflow.operators.bash_operator import BashOperator
from airflow.operators.dummy_operator import DummyOperator
from airflow.operators.python_operator import BranchPythonOperator
from airflow.operators.python_operator import PythonOperator

import environment_config
from gcs_copy_operator import GoogleCloudStorageCopyOperator

default_args = {
    'owner': 'rkrishnap',
    'depends_on_past': False,
    # This is the date to when the airflow pipeline thinks the run started
    # There is some airflow weirdness, for periodic jobs start_date needs to
    # be greater than the interval between jobs
    'start_date': datetime.datetime.now() - datetime.timedelta(days=1, minutes=15),
    'email': environment_config.EMAIL_LIST,
    'email_on_failure': True,
    'email_on_retry': False,
    'retries': 1,
    'retry_delay': datetime.timedelta(minutes=5),
}

def GetSettingPython(ti, setting):
  """Get setting form the generate_flow_args task.

  Args:
    ti: (task_instance) This is provided by the environment
    setting: (string) The name of the setting.
  Returns:
    The item saved in xcom.
  """
  return ti.xcom_pull(task_ids='generate_workflow_args')[setting]


def GetSettingTemplate(setting):
  """Create the template that will resolve to a setting from xcom.

  Args:
    setting: (string) The name of the setting.
  Returns:
    A templated string that resolves to a setting from xcom.
  """
  return ('{{ task_instance.xcom_pull(task_ids="generate_workflow_args"'
          ').%s }}') % (
              setting)


def GetVariableOrDefault(var, default):
  try:
    return Variable.get(var)
  except KeyError:
    return default

def MakeCommonDag(dag_args_func, name,
                  schedule_interval='15 9 * * *'):
  """Creates the shared part of the daily/release dags.
        schedule_interval is in cron format '15 9 * * *')"""
  common_dag = DAG(
      name,
      catchup=False,
      default_args=default_args,
      schedule_interval=schedule_interval,
  )
  tasks = dict()

  generate_flow_args = PythonOperator(
      task_id='generate_workflow_args',
      python_callable=dag_args_func,
      provide_context=True,
      dag=common_dag,
  )
  tasks['generate_workflow_args'] = generate_flow_args

  get_git_commit_cmd = """
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    git clone {{ settings.MFEST_URL }} green-builds || exit 2

    pushd green-builds
    git checkout {{ settings.BRANCH }}
    git checkout {{ settings.MFEST_COMMIT }} || exit 3
    ISTIO_SHA=`grep {{ settings.GITHUB_ORG }}/{{ settings.GITHUB_REPO }} {{ settings.MFEST_FILE }} | cut -f 6 -d \\"` || exit 4
    API_SHA=`  grep {{ settings.GITHUB_ORG }}/api                        {{ settings.MFEST_FILE }} | cut -f 6 -d \\"` || exit 5
    PROXY_SHA=`grep {{ settings.GITHUB_ORG }}/proxy                      {{ settings.MFEST_FILE }} | cut -f 6 -d \\"` || exit 6
    if [ -z ${ISTIO_SHA} ] || [ -z ${API_SHA} ] || [ -z ${PROXY_SHA} ]; then
      echo "ISTIO_SHA:$ISTIO_SHA API_SHA:$API_SHA PROXY_SHA:$PROXY_SHA some shas not found"
      exit 7
    fi
    popd #green-builds

    git clone {{ settings.ISTIO_REPO }} istio-code -b {{ settings.BRANCH }}
    pushd istio-code/release
    ISTIO_HEAD_SHA=`git rev-parse HEAD`
    git checkout ${ISTIO_SHA} || exit 8

    TS_SHA=` git show -s --format=%ct ${ISTIO_SHA}`
    TS_HEAD=`git show -s --format=%ct ${ISTIO_HEAD_SHA}`
    DIFF_SEC=$((TS_HEAD - TS_SHA))
    DIFF_DAYS=$(($DIFF_SEC/86400))
    if [ "{{ settings.CHECK_GREEN_SHA_AGE }}" = "true" ] && [ "$DIFF_DAYS" -gt "2" ]; then
       echo ERROR: ${ISTIO_SHA} is $DIFF_DAYS days older than head of branch {{ settings.BRANCH }}
       exit 9
    fi
    popd #istio-code/release

    if [ "{{ settings.VERIFY_CONSISTENCY }}" = "true" ]; then
      PROXY_REPO=`dirname {{ settings.ISTIO_REPO }}`/proxy
      echo $PROXY_REPO
      git clone $PROXY_REPO proxy-code -b {{ settings.BRANCH }}
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
    gsutil cp *.sh   gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/
    gsutil cp *.json gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/
    popd #istio-code/release

    pushd green-builds
    git rev-parse HEAD
    """

  get_git_commit = BashOperator(
      task_id='get_git_commit',
      bash_command=get_git_commit_cmd,
      xcom_push=True,
      dag=common_dag)
  tasks['get_git_commit'] = get_git_commit

  build_template = """
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    {% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
    # TODO: Merge these json/script changes into istio/istio master and change these path back.
    # Currently we did changes and push those to a test-version folder manually
    gsutil cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/*.json .
    gsutil cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/*.sh .
    chmod u+x *
    ./start_gcb_build.sh -w -p {{ settings.PROJECT_ID \
    }} -r {{ settings.GCR_STAGING_DEST }} -s {{ settings.GCS_BUILD_PATH }} \
    -v "{{ settings.VERSION }}" \
    -u "{{ settings.MFEST_URL }}" \
    -t "{{ m_commit }}" -m "{{ settings.MFEST_FILE }}" \
    -a {{ settings.SVC_ACCT }}
    """
  # NOTE: if you add commands to build_template after start_gcb_build.sh then take care to preserve its return value

  build = BashOperator(
      task_id='run_cloud_builder', bash_command=build_template, dag=common_dag)
  tasks['run_cloud_builder'] = build

  test_command = """
    cp /home/airflow/gcs/data/githubctl ./githubctl
    chmod u+x ./githubctl
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    ls -l    ./githubctl
    ./githubctl \
    --token_file="{{ settings.TOKEN_FILE }}" \
    --op=dailyRelQual \
    --hub=gcr.io/{{ settings.GCR_STAGING_DEST }} \
    --gcs_path="{{ settings.GCS_BUILD_PATH }}" \
    --tag="{{ settings.VERSION }}" \
    --base_branch="{{ settings.BRANCH }}"
    """

  run_release_qualification_tests = BashOperator(
      task_id='run_release_qualification_tests',
      bash_command=test_command,
      retries=0,
      dag=common_dag)
  tasks['run_release_qualification_tests'] = run_release_qualification_tests

  modify_values_command = """
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    gsutil cp gs://istio-release-pipeline-data/release-tools/test-version/data/release/modify_values.sh .
    chmod u+x modify_values.sh
    echo "PIPELINE TYPE is {{ settings.PIPELINE_TYPE }}"
    if [ "{{ settings.PIPELINE_TYPE }}" = "daily" ]; then
        hub="gcr.io/{{ settings.GCR_STAGING_DEST }}"
    elif [ "{{ settings.PIPELINE_TYPE }}" = "monthly" ]; then
        hub="docker.io/istio"
    fi
    ./modify_values.sh -h "${hub}" -t {{ settings.VERSION }} -p gs://{{ settings.GCS_BUILD_BUCKET }}/{{ settings.GCS_STAGING_PATH }} -v {{ settings.VERSION }}
    """

  modify_values = BashOperator(
    task_id='modify_values_helm', bash_command=modify_values_command, dag=common_dag)
  tasks['modify_values_helm'] = modify_values


  copy_files = GoogleCloudStorageCopyOperator(
      task_id='copy_files_for_release',
      source_bucket=GetSettingTemplate('GCS_BUILD_BUCKET'),
      source_object=GetSettingTemplate('GCS_STAGING_PATH'),
      destination_bucket=GetSettingTemplate('GCS_STAGING_BUCKET'),
      dag=common_dag,
  )
  tasks['copy_files_for_release'] = copy_files

  return common_dag, tasks
