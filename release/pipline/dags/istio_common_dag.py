"""Airfow DAG and helpers used in one or more istio release pipline."""
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
import time

from airflow import DAG
from airflow.models import Variable
from airflow.operators.bash_operator import BashOperator
from airflow.operators.python_operator import PythonOperator

import environment_config
from gcs_copy_operator import GoogleCloudStorageCopyOperator

YESTERDAY = datetime.datetime.combine(
    datetime.datetime.today() - datetime.timedelta(days=1),
    datetime.datetime.min.time())

default_args = {
    'owner': 'laane',
    'depends_on_past': False,
    # This is the date to when the airlfow pipline tryes to backfil to.
    'start_date': YESTERDAY,
    'email': ['laane@google.com'],
    'email_on_failure': False,
    'email_on_retry': False,
    'retries': 1,
    'retry_delay': datetime.timedelta(minutes=5),
}


def GetSettingPython(ti, setting):
  """Get setting form the generate_flow_args task.

  Args:
    ti: (task_instance) This is provided by the environment
    setting: (string) The name of the setting.
  Return:
    The item saved in xcom.
  """
  return ti.xcom_pull(task_id=generate_flow_args.task_id)[setting]


def GetSettingTemplate(setting):
  """Create the template that will resolve to a setting from xcom.

  Args:
    setting: (string) The name of the setting.
  Return:
    A templated string that resolves to a setting from xcom.
  """
  return ('{{ task_instance.xcom_pull(task_ids="generate_workflow_args"'
          ').%s }}') % (setting)


def MakeCommonDag(name='istio_daily_flow_test',
                  schedule_interval='15 3 * * *',
                  monthly=False):
  common_dag = DAG(
      name,
      default_args=default_args,
      schedule_interval=schedule_interval,
  )

  def AirflowGetVariableOrBaseCase(var, base):
    try:
      return Variable.get(var)
    except KeyError:
      return base

  def GenerateTestArgs(**kwargs):
    conf = kwargs['dag_run'].conf
    if conf is None:
      conf = dict()

    date = kwargs['execution_date']

    timestamp = time.mktime(date.timetuple())

    minor_version = (date.year - 2017) * 12 + (date.month - 1) - 7
    major_version = AirflowGetVariableOrBaseCase('major_version', 0)
    # This code gets information about the last released version so we know
    # What version number to use for this round.
    r_minor = int(AirflowGetVariableOrBaseCase('released_version_minor', 0))
    r_patch = int(AirflowGetVariableOrBaseCase('released_version_patch', 0))
    # If  we have already released a monthy for this mounth then bump
    # The patch number for the remander of the month.
    if r_minor == minor_version:
      patch = r_patch + 1
    else:
      patch = 0

    if not monthly:
      version = conf.get('VERSION')
    else:
      version = '{}.{}.{}'.format(major_version, minor_version, patch)
    default_conf = environment_config.get_airflow_config(
        version,
        timestamp,
        major=major_version,
        minor=minor_version,
        patch=patch,
        date=date.strftime('%Y%m%d'),
        rc=date.strftime('%H-%M-%S'))
    config_settings = dict(VERSION = default_conf['VERSION'])
    config_settings_name = ['PROJECT_ID','MFEST_URL','MFEST_FILE','GCR_BUCKET','GCS_BUCKET','GCS_PATH','GCR_DEST','SVC_ACCT','GITHUB_ORG','GITHUB_REPO','GCS_GITHUB_PATH','TOKEN_FILE','GCR_DEST','GCS_DEST','DOCKER_HUB','BUILD_GCS_BUCKET',]

    for name in config_settings_name:
      config_settings[name] = conf.get(name) or default_conf[name]

    if monthly:
      MFEST_COMMIT = conf.get('MFEST_COMMIT') or Variable.get('last_daily')
    else:
      MFEST_COMMIT = conf.get('MFEST_COMMIT') or default_conf['MFEST_COMMIT']

    config_settings['BUILD_GCS_PATH'] = '{}/{}'.format(config_settings['BUILD_GCS_BUCKET'], config_settings['GCS_PATH'])
    config_settings['GCS_SOURCE'] =  '{}/{}'.format(config_settings['GCS_BUCKET'], config_settings['GCS_PATH'])

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
    cd green-builds
    git checkout {{ settings.MFEST_COMMIT }} || exit 3
    git rev-parse HEAD
    """

  get_git_commit = BashOperator(
      task_id='get_git_commit',
      bash_command=get_git_commit_cmd,
      xcom_push=True,
      dag=common_dag)

  build_template = """
    chmod +x /home/airflow/gcs/data/release/*
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    {% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
    /home/airflow/gcs/data/release/start_gcb_build.sh -w -p {{ settings.PROJECT_ID \
    }} -r {{ settings.GCR_BUCKET }} -s {{ settings.BUILD_GCS_PATH }} \
    -v "{{ settings.VERSION }}" \
    -u "{{ settings.MFEST_URL }}" \
    -t "{{ m_commit }}" -m "{{ settings.MFEST_FILE }}" \
    -a {{ settings.SVC_ACCT }}
    """

  build = BashOperator(
      task_id='run_cloud_builder', bash_command=build_template, dag=common_dag)

  test_command = """
    chmod +x /home/airflow/gcs/data/githubctl
    {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
    git config --global user.name "TestRunnerBot"
    git config --global user.email "testrunner@istio.io"
    /home/airflow/gcs/data/githubctl \
    --token_file="{{ settings.TOKEN_FILE }}" \
    --op=dailyRelQual \
    --hub=gcr.io/{{ settings.GCR_BUCKET }} \
    --gcs_path="{{ settings.BUILD_GCS_PATH }}" \
    --tag="{{ settings.VERSION }}"
    """

  run_release_quilification_tests = BashOperator(
      task_id='run_release_quilification_tests',
      bash_command=test_command,
      params={'tests': 5},
      retries=2,
      dag=common_dag)
  copy_files = GoogleCloudStorageCopyOperator(
      task_id='copy_files_for_release',
      source_bucket=GetSettingTemplate('BUILD_GCS_BUCKET'),
      source_object=GetSettingTemplate('GCS_PATH'),
      destination_bucket=GetSettingTemplate('GCS_BUCKET'),
      destination_object=GetSettingTemplate('GCS_PATH'),
      dag=common_dag,
  )
  generate_flow_args >> get_git_commit >> build
  run_release_quilification_tests.set_upstream(build)
  run_release_quilification_tests >> copy_files
  return common_dag, copy_files


def ReportDailySuccessful(task_instance, **kwargs):
  date = kwargs['execution_date']
  last_run = float(Variable.get('last_daily_timestamp'))

  timestamp = time.mktime(date.timetuple())
  if timestamp > last_run:
    Variable.set('last_daily_timestamp', timestamp)
    run_sha = task_instance.xcom_pull(task_id='get_git_commit')
    last_version = GetSettingPython(task_instance, 'VERSTION')
    print 'setting last green daily to: {}'.format(run_sha)
    Variable.set('last_sha', run_sha)
    Variable.set('last_daily', last_version)


def MakeMarkComplete(dag):
  return PythonOperator(
      task_id='mark_complete',
      python_callable=ReportDailySuccessful,
      provide_context=True,
      dag=dag,
  )
