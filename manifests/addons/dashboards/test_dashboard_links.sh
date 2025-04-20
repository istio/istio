#!/bin/bash

# Copyright Istio Authors
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#    http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


# This script tests that all dashboard links in the generated dashboards use UIDs instead of paths
# It should be run after ./gen.sh to verify that the dashboard links are correctly generated

set -eu

DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
GEN_DIR="${DIR}"

function check_json_for_path_links() {
  local file=$1
  echo "Checking $file for path-based links..."
  
  # Look for links using paths instead of UIDs - these should not exist
  if grep -q '/dashboard/db/' "$file"; then
    echo "ERROR: Found deprecated path-based dashboard link in $file"
    grep -n '/dashboard/db/' "$file" | head -5
    return 1
  fi
  
  # Look for links using new UID format - these should exist for dashboard links
  if grep -q '/d/' "$file" || grep -q '"dashboards"' "$file"; then
    echo "Found proper UID-based dashboard links in $file"
    return 0
  else
    # If no dashboard links found, just inform but don't fail
    echo "INFO: No dashboard links found in $file"
    return 0
  fi
}

error_count=0

# Check all generated JSON dashboards
for file in "${GEN_DIR}"/*.gen.json "${GEN_DIR}"/*.json; do
  [[ -f "$file" ]] || continue
  
  if ! check_json_for_path_links "$file"; then
    ((error_count++))
  fi
done

if [[ $error_count -gt 0 ]]; then
  echo ""
  echo "Found $error_count dashboards with deprecated path-based links"
  echo "Please update the dashboards to use UID-based links"
  exit 1
fi

echo "All dashboards use the proper UID-based linking format!"
exit 0
