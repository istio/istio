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

dag, copy_files = istio_common_dag.MakeCommonDag(
    'istio_monthly_release',
    schedule_interval=environment_config.MONTHLY_RELEASE_TRIGGER,
    monthly=True)

monthly_release_template = """
{% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
gsutil cp gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/*.json .
gsutil cp gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/*.sh .
chmod u+x *
./start_gcb_publish.sh \
-p "{{ settings.RELEASE_PROJECT_ID }}" -a "{{ settings.SVC_ACCT }}"  \
-v "{{ settings.VERSION }}" -s "{{ settings.GCS_FULL_STAGING_PATH }}" \
-b "{{ settings.GCS_MONTHLY_RELEASE_PATH }}" -r "{{ settings.GCR_RELEASE_DEST }}" \
-g "{{ settings.GCS_GITHUB_PATH }}" -u "{{ settings.MFEST_URL }}" \
-t "{{ m_commit }}" -m "{{ settings.MFEST_FILE }}" \
-h "{{ settings.GITHUB_ORG }}" -i "{{ settings.GITHUB_REPO }}" \
-d "{{ settings.DOCKER_HUB}}" -w
"""

push_release_to_github = BashOperator(
    task_id='github_and_docker_release',
    bash_command=monthly_release_template,
    dag=dag)

daily_release_tag_github_template = """
{% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}
gsutil cp gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/*.json .
gsutil cp gs://{{ settings.GCS_RELEASE_TOOLS_PATH }}/data/release/*.sh .
chmod u+x *
./start_gcb_tag.sh \
-p "{{ settings.RELEASE_PROJECT_ID }}" \
-h "{{ settings.GITHUB_ORG }}" -a "{{ settings.SVC_ACCT }}"  \
-v "{{ settings.VERSION }}"   -e "istio_releaser_bot@example.com" \
-n "IstioReleaserBot" -s "{{ settings.GCS_FULL_STAGING_PATH }}" \
-g "{{ settings.GCS_GITHUB_PATH }}" -u "{{ settings.MFEST_URL }}" \
-t "{{ m_commit }}" -m "{{ settings.MFEST_FILE }}" -w
"""

github_tag_repos = BashOperator(
    task_id='github_tag_repos',
    bash_command=daily_release_tag_github_template,
    dag=dag)

copy_files >> push_release_to_github >> github_tag_repos


def ReportMonthlySuccessful(task_instance, **kwargs):
  del kwargs
  version = istio_common_dag.GetSettingPython(task_instance, 'VERSION')
  try:
    match = re.match(r'([0-9])\.([0-9])\.([0-9]).*', version)
    major, minor, patch = match.group(1), match.group(2), match.group(3)
    Variable.set('major_version', major)
    Variable.set('released_version_minor', minor)
    Variable.set('released_version_patch', patch)
  except (IndexError, AttributeError):
    logging.error('Could not extract released version infomation. \n'
                  'Please set airflow version Variables manually.'
                  'After you are done hit Mark Success.')


make_compleate = PythonOperator(
    task_id='mark_monthly_complete',
    python_callable=ReportMonthlySuccessful,
    provide_context=True,
    dag=dag,
)

github_tag_repos >> make_compleate
