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
    'owner': 'laane',
    'depends_on_past': False,
    # This is the date to when the airlfow pipeline thinks the run started
    'start_date': datetime.datetime.now(),
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


def MakeCommonDag(name='istio_daily_flow_test',
                  schedule_interval='15 9 * * *',
                  monthly=False):
  """Creates the shared part of the daily/monthly dags."""
  common_dag = DAG(
      name,
      catchup=False,
      default_args=default_args,
      schedule_interval=schedule_interval,
  )

  def AirflowGetVariableOrBaseCase(var, base):
    try:
      return Variable.get(var)
    except KeyError:
      return base

  def GenerateTestArgs(**kwargs):
    """Loads the configuration that will be used for this Iteration."""
    conf = kwargs['dag_run'].conf
    if conf is None:
      conf = dict()

    """ Airflow gives the execution date when the job is supposed to be run,
        however we dont backfill and only need to run one build therefore use
        the current date instead of the date that is passed in """
#    date = kwargs['execution_date']
    date = datetime.datetime.now()

    timestamp = time.mktime(date.timetuple())

    # Monthly releases started in Nov 2017 with 0.3.0, so minor is # of months
    # from Aug 2017.
    minor_version = (date.year - 2017) * 12 + (date.month - 1) - 7
    major_version = AirflowGetVariableOrBaseCase('major_version', 0)
    # This code gets information about the latest released version so we know
    # What version number to use for this round.
    r_minor = int(AirflowGetVariableOrBaseCase('released_version_minor', 0))
    r_patch = int(AirflowGetVariableOrBaseCase('released_version_patch', 0))
    # If  we have already released a monthy for this mounth then bump
    # The patch number for the remander of the month.
    if r_minor == minor_version:
      patch = r_patch + 1
    else:
      patch = 0
    # If version is overriden then we should use it otherwise we use it's
    # default or monthly value.
    version = conf.get('VERSION')
    if monthly and not version:
      version = '{}.{}.{}'.format(major_version, minor_version, patch)

    default_conf = environment_config.get_airflow_config(
        version,
        timestamp,
        major=major_version,
        minor=minor_version,
        patch=patch,
        date=date.strftime('%Y%m%d'),
        rc=date.strftime('%H-%M'))
    config_settings = dict(VERSION=default_conf['VERSION'])
    config_settings_name = [
        'PROJECT_ID',
        'MFEST_URL',
        'MFEST_FILE',
        'GCS_STAGING_BUCKET',
        'SVC_ACCT',
        'GITHUB_ORG',
        'GITHUB_REPO',
        'GCS_GITHUB_PATH',
        'TOKEN_FILE',
        'GCR_STAGING_DEST',
        'GCR_RELEASE_DEST',
        'GCS_MONTHLY_RELEASE_PATH',
        'DOCKER_HUB',
        'GCS_BUILD_BUCKET',
        'RELEASE_PROJECT_ID',
    ]

    for name in config_settings_name:
      config_settings[name] = conf.get(name) or default_conf[name]

    if monthly:
      config_settings['MFEST_COMMIT'] = conf.get(
          'MFEST_COMMIT') or Variable.get('latest_sha')
      gcs_path = conf.get('GCS_MONTHLY_STAGE_PATH')
      if not gcs_path:
        gcs_path = default_conf['GCS_MONTHLY_STAGE_PATH']
    else:
      config_settings['MFEST_COMMIT'] = conf.get(
          'MFEST_COMMIT') or default_conf['MFEST_COMMIT']
      gcs_path = conf.get('GCS_DAILY_PATH') or default_conf['GCS_DAILY_PATH']

    config_settings['GCS_STAGING_PATH'] = gcs_path
    config_settings['GCS_BUILD_PATH'] = '{}/{}'.format(
        config_settings['GCS_BUILD_BUCKET'], gcs_path)
    config_settings['GCS_RELEASE_TOOLS_PATH'] = '{}/release-tools/{}'.format(
        config_settings['GCS_BUILD_BUCKET'], gcs_path)
    config_settings['GCS_FULL_STAGING_PATH'] = '{}/{}'.format(
        config_settings['GCS_STAGING_BUCKET'], gcs_path)
    config_settings['ISTIO_REPO'] = 'https://github.com/{}/{}.git'.format(
        config_settings['GITHUB_ORG'], config_settings['GITHUB_REPO'])

    return config_settings

  generate_flow_args = PythonOperator(
      task_id='generate_workflow_args',
      python_callable=GenerateTestArgs,
      provide_context=True,
      dag=common_dag,
  )

  get_git_commit_cmd = """
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    git clone {{ settings.MFEST_URL }} green-builds || exit 2
    pushd green-builds
    git checkout {{ settings.MFEST_COMMIT }} || exit 5
    SHA=`grep {{ settings.GITHUB_ORG }}/{{ settings.GITHUB_REPO }} {{ settings.MFEST_FILE }} | cut -f 6 -d \\"` || exit 3
    if [ -z ${SHA} ]; then
      echo "SHA not found"
      exit 6
    fi
    popd
    git clone {{ settings.ISTIO_REPO }} istio-code
    pushd istio-code/release
    git checkout ${SHA} || exit 4
    gsutil cp *.sh gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/
    gsutil cp *.json gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/
    popd
    pushd green-builds
    git rev-parse HEAD
    """

  get_git_commit = BashOperator(
      task_id='get_git_commit',
      bash_command=get_git_commit_cmd,
      xcom_push=True,
      dag=common_dag)

  build_template = """
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    {% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
    gsutil cp gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/*.json .
    gsutil cp gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/*.sh .
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
    --tag="{{ settings.VERSION }}"
    """

  run_release_qualification_tests = BashOperator(
      task_id='run_release_qualification_tests',
      bash_command=test_command,
      retries=0,
      dag=common_dag)
  copy_files = GoogleCloudStorageCopyOperator(
      task_id='copy_files_for_release',
      source_bucket=GetSettingTemplate('GCS_BUILD_BUCKET'),
      source_object=GetSettingTemplate('GCS_STAGING_PATH'),
      destination_bucket=GetSettingTemplate('GCS_STAGING_BUCKET'),
      dag=common_dag,
  )
  generate_flow_args >> get_git_commit >> build
  run_release_qualification_tests.set_upstream(build)
  run_release_qualification_tests >> copy_files
  return common_dag, copy_files


def ReportDailySuccessful(task_instance, **kwargs):
  """Set this release as the candidate if it is the latest."""
  date = kwargs['execution_date']
  latest_run = float(Variable.get('latest_daily_timestamp'))

  timestamp = time.mktime(date.timetuple())
  logging.info('Current run\'s timestamp: %s \n'
               'latest_daily\'s timestamp: %s', timestamp, latest_run)
  if timestamp >= latest_run:
    Variable.set('latest_daily_timestamp', timestamp)
    run_sha = task_instance.xcom_pull(task_ids='get_git_commit')
    latest_version = GetSettingPython(task_instance, 'VERSION')
    logging.info('setting latest green daily to: %s', run_sha)
    Variable.set('latest_sha', run_sha)
    Variable.set('latest_daily', latest_version)
    logging.info('latest_sha test to %s', run_sha)
    return 'tag_daily_gcr'
  return 'skip_tag_daily_gcr'


gcr_tag_success = r"""
{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
gsutil ls gs://{{ settings.GCS_STAGING_PATH }}/docker/ | \
  grep -Eo "docker\/(([a-z]|-)*).tar.gz" | \
  sed -E "s/docker\/(([a-z]|-)*).tar.gz/\1/g" | \
  while read -r docker_image;do  \
    yes | gcloud container images add-tag \
    "gcr.io/{{ settings.GCR_STAGING_DEST }}/${docker_image}:{{ settings.VERSTION }}" \
    "gcr.io/{{ settings.GCR_STAGING_DEST }}/${docker_image}:latest_daily";
  done
"""


def MakeMarkComplete(dag):
  """Make the final sequence of the daily graph."""
  mark_complete = BranchPythonOperator(
      task_id='mark_complete',
      python_callable=ReportDailySuccessful,
      provide_context=True,
      dag=dag,
  )

  tag_daily_grc = BashOperator(
      task_id='tag_daily_gcr',
      bash_command=gcr_tag_success,
      dag=dag,
  )
  # skip_grc = DummyOperator(
  #     task_id='skip_tag_daily_gcr',
  #     dag=dag,
  # )
  # end = DummyOperator(
  #     task_id='end',
  #     dag=dag,
  #     trigger_rule="one_success",
  # )
  mark_complete >> tag_daily_grc
  # mark_complete >> skip_grc >> end
  return mark_complete
