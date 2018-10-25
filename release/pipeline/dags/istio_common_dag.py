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


def getBashSettingsTemplate(extra_param_lst=[]):
  # import for all other steps
  settings_use_copied_scripts_str = """
                {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
                GCB_ENV_FILE="$(mktemp /tmp/gcb_env_file.XXXXXX)"
                export PROJECT_ID={{ settings.PROJECT_ID }}
                export SVC_ACCT={{ settings.SVC_ACCT }}
                export CB_GCS_RELEASE_TOOLS_PATH={{ settings.CB_GCS_RELEASE_TOOLS_PATH }}
                gsutil -q cp "gs://${CB_GCS_RELEASE_TOOLS_PATH}/gcb_env.sh" "${GCB_ENV_FILE}"
                source "${GCB_ENV_FILE}"
                # use airflow scripts from bootstrap
                gsutil -m cp "gs://${CB_GCS_BUILD_BUCKET}/release-tools/bootstrap/*sh" .
                # use bootstrap json file for get_git_commit task
                gsutil -q cp "gs://${CB_GCS_BUILD_BUCKET}/release-tools/bootstrap/get_commit.template.json" .
                # everything else uses json files saved for this build
                gsutil -mq cp "gs://${CB_GCS_RELEASE_TOOLS_PATH}"/gcb/*json .
                source airflow_scripts.sh
                """
  settings_init_str = """
                {% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
                GCB_ENV_FILE="$(mktemp /tmp/gcb_env_file.XXXXXX)"
                export PROJECT_ID={{ settings.PROJECT_ID }}
                export SVC_ACCT={{ settings.SVC_ACCT }}
                export CB_GCS_RELEASE_TOOLS_PATH={{ settings.CB_GCS_RELEASE_TOOLS_PATH }}
                """

  def getGcbEnvInitTemplate():
    keys = environment_config.GetDefaultAirflowConfigKeys()
    for key in extra_param_lst:
      keys.append(key)
    #end lists loop
    keys.sort() # sort for readability

    gcb_exp_list = [settings_init_str]
    gcb_exp_list.append("""cat << EOF > "${GCB_ENV_FILE}" """)
    for key in keys:
      # only export CB_ variables to gcb
      if key.startswith("CB_"):
        gcb_exp_list.append("export %s={{ settings.%s }}" % (key, key))
    gcb_exp_list.append("EOF")
    gcb_exp_list.append("""
                gsutil -q cp "${GCB_ENV_FILE}" "gs://${CB_GCS_RELEASE_TOOLS_PATH}/gcb_env.sh" """)

    return "\n".join(gcb_exp_list)

  settings_init_gcb_env_str = getGcbEnvInitTemplate()
  return settings_init_gcb_env_str, settings_use_copied_scripts_str


def MergeEnvironmentIntoConfig(env_conf, *config_dicts):
    config_settings = dict()
    for cur_conf_dict in config_dicts:
      for name, value in cur_conf_dict.items():
        config_settings[name] = env_conf.get(name) or value

    return config_settings


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
  init_gcb_env_cmd, copy_env_from_gcb_prefix = getBashSettingsTemplate(extra_param_lst)

  def addAirflowInitBashOperator(task_id):
    task = BashOperator(
      task_id=task_id, bash_command=init_gcb_env_cmd, dag=common_dag)
    tasks[task_id] = task
    return

  def addAirflowBashOperator(cmd_name, task_id, **kwargs):
    cmd = copy_env_from_gcb_prefix + "\ntype %s\n     %s" % (cmd_name, cmd_name)
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

  addAirflowInitBashOperator('init_gcb_env')
  addAirflowBashOperator('get_git_commit_cmd', 'get_git_commit')
  addAirflowBashOperator('build_template', 'run_cloud_builder')
  addAirflowBashOperator('test_command', 'run_release_qualification_tests', retries=0)
  addAirflowBashOperator('modify_values_command', 'modify_values_helm')

  copy_files = GoogleCloudStorageCopyOperator(
      task_id='copy_files_for_release',
      source_bucket=GetSettingTemplate('CB_GCS_BUILD_BUCKET'),
      source_object=GetSettingTemplate('CB_GCS_STAGING_PATH'),
      destination_bucket=GetSettingTemplate('CB_GCS_STAGING_BUCKET'),
      dag=common_dag,
  )
  tasks['copy_files_for_release'] = copy_files

  return common_dag, tasks, addAirflowBashOperator
