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

if [[ "$CB_GITHUB_ORG" != "istio" ]]; then
  if [[ "$CB_BRANCH" == *"release"* ]] || [[ "$CB_BRANCH" == "master" ]]; then
    echo "not messing up daily builds with testing"
    exit 0
  fi
fi

cd /workspace || exit 1
# run the release steps
./rel_push_docker_daily.sh
./rel_daily_complete.sh
