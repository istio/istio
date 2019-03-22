#!/bin/bash

# Copyright 2018 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

set -x

gsutil -q rm -rf "gs://$CB_GCS_FULL_STAGING_PATH" || echo "Staging path does not exist."
#copy files over to final destination
gsutil -m cp -r "gs://$CB_GCS_BUILD_PATH" "gs://$CB_GCS_FULL_STAGING_PATH"


cd /workspace || exit 1
# for testing use your own GITHUB_ORG (your own private org)
# also set CB_TEST_GITHUB_TOKEN_FILE_PATH so that your github creds are used

# run the release steps
./github_publish_release.sh
./github_tag_release.sh
