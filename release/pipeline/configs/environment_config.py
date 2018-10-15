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

# fixed airflow configuration for the istio dags
airflow_fixed_config = dict(
    CB_CHECK_GREEN_SHA_AGE='true',
    CB_GCR_STAGING_DEST='istio-release',
    CB_GCS_BUILD_BUCKET='istio-release-pipeline-data',
    CB_GCS_STAGING_BUCKET='istio-prerelease',
    CB_GITHUB_ORG='istio',
    PROJECT_ID='istio-io',
    SVC_ACCT='202987436673-compute@developer.gserviceaccount.com')


def GetDefaultAirflowConfig(branch, commit, docker_hub, gcs_path, github_org, pipeline_type,
			verify_consistency, version):
  """Return a dict of the configuration for the Pipeline."""
  config = dict(airflow_fixed_config)

  # direct config
  config['CB_BRANCH']             = branch
  config['CB_DOCKER_HUB']         = docker_hub # e.g. docker.io/istio
  # istioctl_docker_hub is the string used by istioctl, helm values, deb file
  config['CB_ISTIOCTL_DOCKER_HUB'] = config['CB_DOCKER_HUB']
  # push_docker_hubs is the set of comma separated hubs to which the code is pushed
  config['CB_PUSH_DOCKER_HUBS']   = config['CB_DOCKER_HUB']
  config['CB_GCS_STAGING_PATH']   = gcs_path
  config['CB_GITHUB_ORG']         = github_org
  config['CB_PIPELINE_TYPE']      = pipeline_type
  config['CB_VERIFY_CONSISTENCY'] = verify_consistency
  config['CB_VERSION']            = version
  config['CB_COMMIT']             = commit


  # derivative and more convoluted config

  # build path is of the form         '{gcs_build_bucket}/{gcs_path}',
  config['CB_GCS_BUILD_PATH']          = '%s/%s' % (config['CB_GCS_BUILD_BUCKET'], gcs_path)
  # full staging path is of the form  '{gcs_staging_bucket}/{gcs_path}',
  config['CB_GCS_FULL_STAGING_PATH']   = '%s/%s' % (config['CB_GCS_STAGING_BUCKET'], gcs_path)
  # release tools path is of the form '{gcs_build_bucket}/release-tools/{gcs_path}',
  config['CB_GCS_RELEASE_TOOLS_PATH']  = '%s/release-tools/%s' % (config['CB_GCS_BUILD_BUCKET'], gcs_path)

  return config


def GetDefaultAirflowConfigKeys():
  """Return a list of the keys of configuration for the Pipeline."""
  dc = GetDefaultAirflowConfig(branch="", commit="", docker_hub="", gcs_path="", github_org = "", pipeline_type="",
			verify_consistency="", version="")
  return list(dc.keys())
