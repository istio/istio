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

#Make the release on the 3ed Thursday of the mounth.
#It will run on at 17:15 GST
MONTHLY_RELEASE_TRIGGER = '15 17 * * 4#3'

AIRFLOW_CONFIG = dict(
    PROJECT_ID='istio-release',
    RELEASE_PROJECT_ID='istio-io',
    MFEST_URL='https://github.com/istio/green-builds',
    MFEST_FILE='build.xml',
    MFEST_COMMIT='master@{{{timestamp}}}',
    VERSION='{major}.{minor}.{patch}-pre{date}-{rc}',
    GCS_BUILD_BUCKET='istio-release-pipeline-data',
    GCS_STAGING_BUCKET='istio-prerelease',
    GCS_DAILY_PATH='daily-build/{VERSION}',
    GCS_MONTHLY_STAGE_PATH='prerelease/{VERSION}',
    GCS_MONTHLY_RELEASE_PATH='istio-release/releases/{VERSION}',
    SVC_ACCT='202987436673-compute@developer.gserviceaccount.com',
    TOKEN_FILE='/var/run/secrets/kubernetes.io/serviceaccount/tokenFile',
    GITHUB_ORG='istio',
    PROD_GITHUB_ORG='istio',
    GITHUB_REPO='istio',
    GCR_STAGING_DEST='istio-release',
    GCR_RELEASE_DEST='istio-io',
    GCS_GITHUB_PATH='istio-secrets/github.txt.enc',
    DOCKER_HUB='istio')


def get_airflow_config(version, timestamp, major, minor, patch, date, rc):
  """Return a dict of the configuration for the Pipeline."""
  config = dict(AIRFLOW_CONFIG)
  if version is not None:
    config['VERSION'] = version
  else:
    config['VERSION'] = config['VERSION'].format(
        major=major, minor=minor, patch=patch, date=date, rc=rc)
  config['MFEST_COMMIT'] = config['MFEST_COMMIT'].format(timestamp=timestamp)
  # This works becuse python format ignores keywork args that arn't pressent.
  for k, v in config.items():
    if k not in ['VERSION', 'MFEST_COMMIT']:
      config[k] = v.format(VERSION=config['VERSION'])
  return config
