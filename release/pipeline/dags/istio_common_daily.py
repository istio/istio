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
  if timestamp >= latest_run:
    run_sha = task_instance.xcom_pull(task_ids='get_git_commit')
    latest_version = istio_common_dag.GetSettingPython(task_instance, 'VERSION')

    Variable.set(branch+'_latest_sha', run_sha)
    Variable.set(branch+'_latest_daily', latest_version)
    Variable.set(branch+'_latest_daily_timestamp', timestamp)

    logging.info('%s_latest_sha set to %s', branch, run_sha)
    logging.info('setting latest green daily of %s branch to: %s', branch, run_sha)
    return 'tag_daily_gcr'
  return 'skip_tag_daily_gcr'


def MakeMarkComplete(dag, addAirflowBashOperator):
  """Make the final sequence of the daily graph."""
  mark_complete = BranchPythonOperator(
      task_id='mark_complete',
      python_callable=ReportDailySuccessful,
      provide_context=True,
      dag=dag,
  )

  addAirflowBashOperator('gcr_tag_success', 'tag_daily_gcr')
  # skip_grc = DummyOperator(
  #     task_id='skip_tag_daily_gcr',
  #     dag=dag,
  # )
  # end = DummyOperator(
  #     task_id='end',
  #     dag=dag,
  #     trigger_rule="one_success",
  # )
  # mark_complete >> skip_grc >> end
  return mark_complete



def DailyPipeline(branch):
  def DailyGenerateTestArgs(**kwargs):
    """Loads the configuration that will be used for this Iteration."""
    conf = kwargs['dag_run'].conf
    if conf is None:
      conf = dict()

    # If variables are overriden then we should use it otherwise we use it's
    # default value.
    date = datetime.datetime.now()
    date_string = date.strftime('%Y%m%d-%H-%M')

    version = conf.get('VERSION')
    if version is None:
      # VERSION is of the form '{branch}-{date_string}'
      version = '%s-%s' % (branch, date_string)

    gcs_path = conf.get('GCS_DAILY_PATH')
    if gcs_path is None:
       # GCS_DAILY_PATH is of the form 'daily-build/{version}'
       gcs_path = 'daily-build/%s' % (version)

    commit = conf.get('COMMIT') or ""

    mfest_commit = conf.get('MFEST_COMMIT')
    if mfest_commit is None:
       timestamp = time.mktime(date.timetuple())
       # MFEST_COMMIT is of the form '{branch}@{{{timestamp}}}',
       mfest_commit = '%s@{%s}' % (branch, timestamp)

    default_conf = environment_config.GetDefaultAirflowConfig(
        branch=branch,
        commit=commit,
        gcs_path=gcs_path,
        mfest_commit=mfest_commit,
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
  tasks['mark_daily_complete'] = MakeMarkComplete(dag, addAirflowBashOperator)

  #tasks['generate_workflow_args']
  tasks['get_git_commit'                 ].set_upstream(tasks['generate_workflow_args'])
  tasks['run_cloud_builder'              ].set_upstream(tasks['get_git_commit'])
  tasks['run_release_qualification_tests'].set_upstream(tasks['run_cloud_builder'])
  tasks['modify_values_helm'             ].set_upstream(tasks['run_release_qualification_tests'])
  tasks['copy_files_for_release'         ].set_upstream(tasks['modify_values_helm'])
  tasks['mark_daily_complete'            ].set_upstream(tasks['copy_files_for_release'])
  tasks['tag_daily_gcr'                  ].set_upstream(tasks['mark_daily_complete'])

  return dag
