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

EMAIL_LIST = ['istio-release@google.com']

AIRFLOW_CONFIG = dict(
    BRANCH='{branch}',
    CHECK_GREEN_SHA_AGE='true',
    GCR_STAGING_DEST='istio-release',
    GCS_BUILD_BUCKET='istio-release-pipeline-data',
    GCS_BUILD_PATH='{gcs_build_bucket}/{gcs_path}',
    GCS_FULL_STAGING_PATH='{gcs_staging_bucket}/{gcs_path}',
    GCS_RELEASE_TOOLS_PATH='{gcs_build_bucket}/release-tools/{gcs_path}',
    GCS_STAGING_BUCKET='istio-prerelease',
    GCS_STAGING_PATH='{gcs_path}',
    GITHUB_ORG='istio',
    GITHUB_REPO='istio',
    ISTIO_REPO='https://github.com/{github_org}/{github_repo}.git',
    MFEST_COMMIT='{mfest_commit}',
    MFEST_FILE='build.xml',
    MFEST_URL='https://github.com/istio/green-builds',
    PROJECT_ID='istio-release',
    SVC_ACCT='202987436673-compute@developer.gserviceaccount.com',
    TOKEN_FILE='/var/run/secrets/kubernetes.io/serviceaccount/tokenFile',
    VERIFY_CONSISTENCY='{verify_consistency}',
    VERSION='{version}')


def get_default_config(branch, gcs_path, mfest_commit,
			verify_consistency, version):
  """Return a dict of the configuration for the Pipeline."""
  config = dict(AIRFLOW_CONFIG)

  config['BRANCH'] = AIRFLOW_CONFIG['BRANCH'].format(branch=branch)
  config['GCS_BUILD_PATH'] = AIRFLOW_CONFIG['GCS_BUILD_PATH'].format(
		gcs_build_bucket=AIRFLOW_CONFIG['GCS_BUILD_BUCKET'], gcs_path=gcs_path)
  config['GCS_FULL_STAGING_PATH'] = AIRFLOW_CONFIG['GCS_FULL_STAGING_PATH'].format(
		gcs_staging_bucket=AIRFLOW_CONFIG['GCS_STAGING_BUCKET'], gcs_path=gcs_path)
  config['GCS_RELEASE_TOOLS_PATH'] = AIRFLOW_CONFIG['GCS_RELEASE_TOOLS_PATH'].format(
		gcs_build_bucket=AIRFLOW_CONFIG['GCS_BUILD_BUCKET'], gcs_path=gcs_path)
  config['GCS_STAGING_PATH'] = AIRFLOW_CONFIG['GCS_STAGING_PATH'].format(
		gcs_path=gcs_path)
  config['ISTIO_REPO'] = AIRFLOW_CONFIG['ISTIO_REPO'].format(
		github_org=AIRFLOW_CONFIG['GITHUB_ORG'],
		github_repo=AIRFLOW_CONFIG['GITHUB_REPO'])
  config['MFEST_COMMIT'] = AIRFLOW_CONFIG['MFEST_COMMIT'].format(
                mfest_commit=mfest_commit)
  config['VERIFY_CONSISTENCY'] = AIRFLOW_CONFIG['VERIFY_CONSISTENCY'].format(
                verify_consistency=verify_consistency)
  config['VERSION'] = AIRFLOW_CONFIG['VERSION'].format(version=version)

  return config
