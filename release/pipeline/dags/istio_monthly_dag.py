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
import logging
import re

from airflow import DAG
from airflow.models import Variable
from airflow.operators.bash_operator import BashOperator
from airflow.operators.python_operator import PythonOperator

import environment_config
import istio_common_dag

monthly_extra_params = ['DOCKER_HUB', 'GCR_RELEASE_DEST', 'GCS_GITHUB_PATH',
                          'RELEASE_PROJECT_ID', 'GCS_MONTHLY_RELEASE_PATH']
def testMonthlyConfigSettings(config_settings):
  tmp_settings = dict(config_settings)
  for key in monthly_extra_params:
    # pop throws keyerror if it cant find key, which is what we want
    tmp_settings.pop(key)
  for key in environment_config.GetDefaultAirflowConfigKeys():
    # pop throws keyerror if it cant find key, which is what we want
    tmp_settings.pop(key)
  if len(tmp_settings) != 0:
    raise ValueError('monthly config settings has unexpected keys')

def MonthlyPipeline():
  MONTHLY_RELEASE_TRIGGER = '15 17 * * 4#3'

  def MonthlyGenerateTestArgs(**kwargs):

    """Loads the configuration that will be used for this Iteration."""
    conf = kwargs['dag_run'].conf
    if conf is None:
      conf = dict()

    # If version is overridden then we should use it otherwise we use it's
    # default or monthly value.
    version = conf.get('VERSION') or istio_common_dag.GetVariableOrDefault('monthly-version', None)
    if not version or version == 'INVALID':
      raise ValueError('version needs to be provided')
    Variable.set('monthly-version', 'INVALID')

    #GCS_MONTHLY_STAGE_PATH is of the form ='prerelease/{version}'
    gcs_path = 'prerelease/%s' % (version)

    branch = conf.get('BRANCH') or istio_common_dag.GetVariableOrDefault('monthly-branch', None)
    if not branch or branch == 'INVALID':
      raise ValueError('branch needs to be provided')
    Variable.set('monthly-branch', 'INVALID')
    commit = conf.get('COMMIT') or branch
    mfest_commit = conf.get('MFEST_COMMIT') or branch

    default_conf = environment_config.GetDefaultAirflowConfig(
        branch=branch,
        commit=commit,
        gcs_path=gcs_path,
        mfest_commit=mfest_commit,
        pipeline_type='monthly',
        verify_consistency='true',
        version=version)

    config_settings = dict()
    for name in default_conf.iterkeys():
      config_settings[name] = conf.get(name) or default_conf[name]

    # These are the extra params that are passed to the dags for monthly release
    monthly_conf = dict()
    monthly_conf['DOCKER_HUB'              ] = 'istio'
    monthly_conf['GCR_RELEASE_DEST'        ] = 'istio-io'
    monthly_conf['GCS_GITHUB_PATH'         ] = 'istio-secrets/github.txt.enc'
    monthly_conf['RELEASE_PROJECT_ID'      ] = 'istio-io'
    # GCS_MONTHLY_RELEASE_PATH is of the form  'istio-release/releases/{version}'
    monthly_conf['GCS_MONTHLY_RELEASE_PATH'] = 'istio-release/releases/%s' % (version)
    for name in monthly_conf.iterkeys():
      config_settings[name] = conf.get(name) or monthly_conf[name]

    testMonthlyConfigSettings(config_settings)
    return config_settings

  def ReportMonthlySuccessful(task_instance, **kwargs):
    del kwargs

  dag, tasks, addAirflowBashOperator = istio_common_dag.MakeCommonDag(
    MonthlyGenerateTestArgs,
    'istio_monthly_dag',
    schedule_interval=MONTHLY_RELEASE_TRIGGER,
    extra_param_lst=monthly_extra_params)

  addAirflowBashOperator('release_push_github_docker_template', 'github_and_docker_release', need_commit=True)
  addAirflowBashOperator('release_tag_github_template', 'github_tag_repos', need_commit=True)

  mark_monthly_complete = PythonOperator(
    task_id='mark_monthly_complete',
    python_callable=ReportMonthlySuccessful,
    provide_context=True,
    dag=dag,
  )
  tasks['mark_monthly_complete'] = mark_monthly_complete

# tasks['generate_workflow_args']
  tasks['get_git_commit'                 ].set_upstream(tasks['generate_workflow_args'])
  tasks['run_cloud_builder'              ].set_upstream(tasks['get_git_commit'])
  tasks['run_release_qualification_tests'].set_upstream(tasks['run_cloud_builder'])
  tasks['modify_values_helm'             ].set_upstream(tasks['run_release_qualification_tests'])
  tasks['copy_files_for_release'         ].set_upstream(tasks['modify_values_helm'])
  tasks['github_and_docker_release'      ].set_upstream(tasks['copy_files_for_release'])
  tasks['github_tag_repos'               ].set_upstream(tasks['github_and_docker_release'])
  tasks['mark_monthly_complete'          ].set_upstream(tasks['github_tag_repos'])

  return dag



dagMonthly = MonthlyPipeline()
dagMonthly
