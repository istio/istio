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
  settings_prefix_str = """
{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
set -x
GCB_ENV_FILE="$(mktemp /tmp/gcb_env_file.XXXXXX)"
"""
  exp_rel_tools_path_str = 'export CB_GCS_RELEASE_TOOLS_PATH={{ settings.CB_GCS_RELEASE_TOOLS_PATH }}'
  upload_gcb_env_str = 'gsutil -q cp "${GCB_ENV_FILE}" "gs://${CB_GCS_RELEASE_TOOLS_PATH}/gcb_env.sh"'
  download_gcb_env_str = 'gsutil -q cp "gs://${CB_GCS_RELEASE_TOOLS_PATH}/gcb_env.sh" "${GCB_ENV_FILE}"'
  airflow_scripts_init_str = """
                source "${GCB_ENV_FILE}"
                # cloning master allows us to use master code to do builds for all releases, if this needs to
                # be changed because of future incompatible changes
                # we just need to clone the compatible SHA here and in get_commit.template.json
                git clone "https://github.com/${CB_GITHUB_ORG}/istio.git" "istio-code" -b "master" --depth 1
                # use release scripts from master
                cp istio-code/release/pipeline/*sh istio-code/release/gcb/json_parse_shared.sh istio-code/release/gcb/*json .
                source airflow_scripts.sh
                """
  airflow_scripts_copy_str = """
                source "${GCB_ENV_FILE}"
                # use scripts saved for this build
                gsutil -mq cp "gs://${CB_GCS_RELEASE_TOOLS_PATH}"/pipeline/*sh .
                gsutil -mq cp "gs://${CB_GCS_RELEASE_TOOLS_PATH}"/gcb/json_parse_shared.sh .
                gsutil -mq cp "gs://${CB_GCS_RELEASE_TOOLS_PATH}"/gcb/*json .
                source airflow_scripts.sh
                """

  def exportNonGcbEnv(keys):
    non_gcb_exp_list = []
    for key in keys:
      # export non CB_ variables locally
      if not key.startswith("CB_"):
        non_gcb_exp_list.append("export %s={{ settings.%s }}" % (key, key))

    return "\n".join(non_gcb_exp_list)

  def exportGcbEnv(keys):
    gcb_exp_list = []
    gcb_env_prefix = """
cat << EOF > "${GCB_ENV_FILE}" """

    gcb_exp_list.append(gcb_env_prefix)
    for key in keys:
      # only export CB_ variables to gcb
      if key.startswith("CB_"):
        gcb_exp_list.append("export %s={{ settings.%s }}" % (key, key))
    gcb_env_suffix = 'EOF'
    gcb_exp_list.append(gcb_env_suffix)

    return "\n".join(gcb_exp_list)

  def getGcbEnvInitTemplate(keys):
    gcb_env_exp_str = exportGcbEnv(keys)
    non_gcb_env_exp_str = exportNonGcbEnv(keys)

    gcb_exp_list = [settings_prefix_str,
                    non_gcb_env_exp_str,
                    exp_rel_tools_path_str,
                    gcb_env_exp_str,
                    upload_gcb_env_str,
                    airflow_scripts_init_str]
    return "\n".join(gcb_exp_list)

  def getCopyFromGcbTemplate(keys):
    non_gcb_env_exp_str = exportNonGcbEnv(keys)

    copy_list = [settings_prefix_str,
                 non_gcb_env_exp_str,
                 exp_rel_tools_path_str,
                 download_gcb_env_str,
                 airflow_scripts_copy_str]
    return "\n".join(copy_list)

  keys = environment_config.GetDefaultAirflowConfigKeys()
  for key in extra_param_lst:
    keys.append(key)
  #end lists loop
  keys.sort() # sort for readability

  init_gcb_env_str = getGcbEnvInitTemplate(keys)
  copy_from_gcb_str = getCopyFromGcbTemplate(keys)

  return init_gcb_env_str, copy_from_gcb_str


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
  init_gcb_env_prefix, copy_from_gcb_prefix = getBashSettingsTemplate(extra_param_lst)

  def addAirflowBashOperator(cmd_name, task_id, **kwargs):
    cmd_list = []
    if cmd_name == "get_git_commit_cmd":
       cmd_list.append(init_gcb_env_prefix)
    else:
       cmd_list.append(copy_from_gcb_prefix)

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
