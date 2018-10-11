"""Airfow DAG and helpers used in one or more istio release pipeline."""
"""Copyright 2018 Istio Authors. All Rights Reserved.

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

import environment_config
import istio_common_dag

def testDailyConfigSettings(config_settings):
  tmp_settings = dict(config_settings)
  for key in environment_config.GetDefaultAirflowConfigKeys():
    # pop throws keyerror if it cant find key, which is what we want
    tmp_settings.pop(key)
  if len(tmp_settings) != 0:
    raise ValueError('daily config settings has unexpected keys')


def ReportDailySuccessful(task_instance, **kwargs):
  """Set this release as the candidate if it is the latest."""
  date = kwargs['execution_date']
  branch = istio_common_dag.GetSettingPython(task_instance, 'BRANCH')
  latest_run = float(istio_common_dag.GetVariableOrDefault(branch+'latest_daily_timestamp', 0))

  timestamp = time.mktime(date.timetuple())
  logging.info("Current run's timestamp: %s \n"
               "latest_daily's timestamp: %s", timestamp, latest_run)

def DailyPipeline(branch):
  def DailyGenerateTestArgs(**kwargs):
    """Loads the configuration that will be used for this Iteration."""
    conf = kwargs['dag_run'].conf
    if conf is None:
      conf = dict()

    # If variables are overridden then we should use it otherwise we use it's
    # default value.
    date = datetime.datetime.now()
    date_string = date.strftime('%Y%m%d-%H-%M')

    docker_hub = conf.get('DOCKER_HUB')
    if docker_hub is None:
      docker_hub = 'gcr.io/istio-release'

    version = conf.get('VERSION')
    if version is None:
      # VERSION is of the form '{branch}-{date_string}'
      version = '%s-%s' % (branch, date_string)

    gcs_path = conf.get('GCS_DAILY_PATH')
    if gcs_path is None:
       # GCS_DAILY_PATH is of the form 'daily-build/{version}'
       gcs_path = 'daily-build/%s' % (version)

    commit = conf.get('COMMIT') or ""

    github_org = conf.get('GITHUB_ORG') or "istio"

    default_conf = environment_config.GetDefaultAirflowConfig(
        branch=branch,
        commit=commit,
        docker_hub=docker_hub,
        gcs_path=gcs_path,
        github_org=github_org,
        pipeline_type='daily',
        verify_consistency='false',
        version=version)

    config_settings = dict()
    for name in default_conf.iterkeys():
      config_settings[name] = conf.get(name) or default_conf[name]

    testDailyConfigSettings(config_settings)
    return config_settings

  dag_name = 'istio_daily_' + branch
  dag, tasks, addAirflowBashOperator = istio_common_dag.MakeCommonDag(
       DailyGenerateTestArgs,
       name=dag_name, schedule_interval='15 9 * * *')
  addAirflowBashOperator('gcr_tag_success', 'tag_daily_gcr')

  #tasks['generate_workflow_args']
  tasks['get_git_commit'                 ].set_upstream(tasks['generate_workflow_args'])
  tasks['run_cloud_builder'              ].set_upstream(tasks['get_git_commit'])
  tasks['modify_values_helm'             ].set_upstream(tasks['run_cloud_builder'])
  tasks['run_release_qualification_tests'].set_upstream(tasks['modify_values_helm'])
  tasks['copy_files_for_release'         ].set_upstream(tasks['run_release_qualification_tests'])
  tasks['tag_daily_gcr'                  ].set_upstream(tasks['copy_files_for_release'])

  return dag
