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

def getBashCommitTemplate():
  commit_template = """{% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
                       export m_commit="{{ m_commit }}" """
  return commit_template

def getBashSettingsTemplate(extra_param_lst=[]):
  keys = environment_config.GetDefaultAirflowConfigKeys()
  for key in extra_param_lst:
    keys.append(key)
  #end lists loop
  keys.sort() # sort for readability

  template_prefix = "{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}"
  template_list = [template_prefix]
  for key in keys:
    template_list.append("export %s={{ settings.%s }}" % (key, key))
  template_list.append("""
                git clone "$ISTIO_REPO" "istio-code" -b "$BRANCH" --depth 1
                pushd "istio-code/release/"
                source ./airflow_scripts.sh""")
  return "\n".join(template_list)


def MakeCommonDag(dag_args_func, name,
                  schedule_interval='15 9 * * *',
                  extra_param_lst=[]):
  """Creates the shared part of the daily/release dags.
        schedule_interval is in cron format '15 9 * * *')"""
  common_dag = DAG(
      name,
      catchup=False,
      default_args=default_args,
      schedule_interval=schedule_interval,
  )

  tasks = dict()
  cmd_template = getBashSettingsTemplate(extra_param_lst)

  def addAirflowBashOperator(cmd_name, task_id, need_commit=False, **kwargs):
    cmd_list = []
    if need_commit == True:
      cmd_list.append(getBashCommitTemplate())
    cmd_list.append(cmd_template)

    cmd_list.append("type %s\n     %s" % (cmd_name, cmd_name))
    cmd = "\n".join(cmd_list)
    task = BashOperator(
      task_id=task_id, bash_command=cmd, dag=common_dag, **kwargs)
    tasks[task_id] = task
    return

  generate_flow_args = PythonOperator(
      task_id='generate_workflow_args',
      python_callable=dag_args_func,
      provide_context=True,
      dag=common_dag,
  )
  tasks['generate_workflow_args'] = generate_flow_args

  addAirflowBashOperator('get_git_commit_cmd', 'get_git_commit', xcom_push=True)
  addAirflowBashOperator('build_template', 'run_cloud_builder', need_commit=True)
  addAirflowBashOperator('test_command', 'run_release_qualification_tests', retries=0)
  addAirflowBashOperator('modify_values_command', 'modify_values_helm')

  copy_files = GoogleCloudStorageCopyOperator(
      task_id='copy_files_for_release',
      source_bucket=GetSettingTemplate('GCS_BUILD_BUCKET'),
      source_object=GetSettingTemplate('GCS_STAGING_PATH'),
      destination_bucket=GetSettingTemplate('GCS_STAGING_BUCKET'),
      dag=common_dag,
  )
  tasks['copy_files_for_release'] = copy_files

  return common_dag, tasks, addAirflowBashOperator
