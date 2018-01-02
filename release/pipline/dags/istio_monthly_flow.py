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

from airflow import DAG
from airflow.models import Variable
from airflow.operators.bash_operator import BashOperator
from airflow.operators.subdag_operator import SubDagOperator

import istio_common_dag

dag, copy_files = istio_common_dag.MakeCommonDag(
    'istio_monthly_release',
    schedule_interval='15 4 20 * *',
    monthly=True)

"""
subdag_task_id = 'build_and_varify'

build_dag, copy_files = istio_common_dag.MakeCommonDag(
    name='{}.{}'.format(dag.dag_id, subdag_task_id),
    schedule_interval=dag.schedule_interval,
    monthly=True)
"""

#daily_subdag = SubDagOperator(
#    task_id=subdag_task_id, subdag=build_dag, dag=dag)


monthly_release_template="""
chmod +x /home/airflow/gcs/data/release/*
{% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}

echo /home/airflow/gcs/data/release/start_gcb_publish.sh \
-p "{{ settings.PROJECT_ID }}" -a "{{ settings.SVC_ACCT }}"  \
-v "{{ settings.VERSION }}" -s "{{ settings.GCS_SOURCE }}" \
-b "{{ settings.GCS_DEST }}" -r "{{ settings.GCR_DEST }}" \
-g "{{ settings.GCS_GITHUB_PATH }}" -u "{{ settings.MFEST_URL }}" \
-t "{{ m_commit }}" -m "{{ settings.MFEST_FILE }}" \
-h "{{ settings.GITHUB_ORG }}" -i "{{ settings.GITHUB_REPO }}" \
-d "{{ settings.DOCKER_HUB}}" -w
"""

push_release_to_github = BashOperator(
    task_id='github_and_docker_release',
    bash_command=monthly_release_template,
    dag=dag)

daily_release_tag_github_template="""
chmod +x /home/airflow/gcs/data/release/*
{% set m_commit = task_instance.xcom_pull(task_ids='get_git_commit') %}
{% set settings = task_instance.xcom_pull(task_ids='generate_workflow_args') %}

echo /home/airflow/gcs/data/release/start_gcb_tag.sh \
-p "{{ settings.PROJECT_ID }}" \
-h "{{ settings.GITHUB_ORG }}" -a "{{ settings.SVC_ACCT }}"  \
-v "{{ settings.VERSION }}"   -e "istio_releaser_bot@example.com" \
-n "IstioReleaserBot" -s "{{ settings.GCS_SOURCE }}" \
-g "{{ settings.GCS_GITHUB_PATH }}" -u "{{ settings.MFEST_URL }}" \
-t "{{ m_commit }}" -m "{{ settings.MFEST_FILE }}" -w
"""

github_tag_repos = BashOperator(
    task_id='github_tag_repos',
    bash_command=daily_release_tag_github_template,
    dag=dag)

copy_files >> push_release_to_github >> github_tag_repos
