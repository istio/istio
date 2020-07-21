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

function remove_istio_cni() {
  local cni_config_file=$1
  local cni_conf_data
  cni_conf_data=$(jq 'del( .plugins[]? | select(.type == "istio-cni"))' < "${cni_config_file}")
  # Rewrite the config file atomically: write into a temp file in the same directory then rename.
  cat > "${cni_config_file}.tmp" <<EOF
${cni_conf_data}
EOF
  mv "${cni_config_file}.tmp" "${cni_config_file}"
}

if [ $# -ne 1 ] || [ -z "$1" ]; then
  echo "No argument supplied or empty: Must be a valid path to the CNI config file."
  exit 1
fi

if [ ! -e "$1" ]; then
  echo "File does not exist: $1"
  exit 1
fi

echo "Removing istio-cni config from CNI chain config in $1"
remove_istio_cni "$1"