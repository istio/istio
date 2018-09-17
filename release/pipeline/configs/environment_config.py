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
    CHECK_GREEN_SHA_AGE='true',
    GCR_STAGING_DEST='istio-release',
    GCS_BUILD_BUCKET='istio-release-pipeline-data',
    GCS_STAGING_BUCKET='istio-prerelease',
    GITHUB_ORG='istio',
    GITHUB_REPO='istio',
    MFEST_FILE='build.xml',
    MFEST_URL='https://github.com/istio/green-builds',
    PROJECT_ID='istio-release',
    SVC_ACCT='202987436673-compute@developer.gserviceaccount.com',
    TOKEN_FILE='/var/run/secrets/kubernetes.io/serviceaccount/tokenFile')


def GetDefaultAirflowConfig(branch, gcs_path, mfest_commit, pipeline_type,
			verify_consistency, version, commit):
  """Return a dict of the configuration for the Pipeline."""
  config = dict(airflow_fixed_config)

  # direct config
  config['BRANCH']             = branch
  config['GCS_STAGING_PATH']   = gcs_path
  config['MFEST_COMMIT']       = mfest_commit
  config['PIPELINE_TYPE']      = pipeline_type
  config['VERIFY_CONSISTENCY'] = verify_consistency
  config['VERSION']            = version
  # MFEST_COMMIT was used for green build, we are transitioning away from it
  # COMMIT is being used for istio/istio commit sha
  config['COMMIT']             = commit



  # derivative and more convoluted config


  # build path is of the form         '{gcs_build_bucket}/{gcs_path}',
  config['GCS_BUILD_PATH']          = '%s/%s' % (config['GCS_BUILD_BUCKET'], gcs_path)
  # full staging path is of the form  '{gcs_staging_bucket}/{gcs_path}',
  config['GCS_FULL_STAGING_PATH']   = '%s/%s' % (config['GCS_STAGING_BUCKET'], gcs_path)
  # release tools path is of the form '{gcs_build_bucket}/release-tools/{gcs_path}',
  config['GCS_RELEASE_TOOLS_PATH']  = '%s/release-tools/%s' % (config['GCS_BUILD_BUCKET'], gcs_path)
  #                                   'https://github.com/{github_org}/{github_repo}.git',
  config['ISTIO_REPO']              = 'https://github.com/%s/%s.git' % (config['GITHUB_ORG'], config['GITHUB_REPO'])

  return config


def GetDefaultAirflowConfigKeys():
  """Return a list of the keys of configuration for the Pipeline."""
  dc = GetDefaultAirflowConfig(branch="", gcs_path="", mfest_commit="", pipeline_type="",
			verify_consistency="", version="", commit="")
  return list(dc.keys())
