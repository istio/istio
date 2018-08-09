#!/bin/bash

# Copyright 2017 Istio Authors

#   Licensed under the Apache License, Version 2.0 (the "License");
#   you may not use this file except in compliance with the License.
#   You may obtain a copy of the License at

#       http://www.apache.org/licenses/LICENSE-2.0

#   Unless required by applicable law or agreed to in writing, software
#   distributed under the License is distributed on an "AS IS" BASIS,
#   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
#   See the License for the specific language governing permissions and
#   limitations under the License.

MASON_CLIENT_PID=-1

function mason_cleanup() {
  if [[ ${MASON_CLIENT_PID} != -1 ]]; then
    kill -SIGINT ${MASON_CLIENT_PID} || echo "failed to kill mason client"
    wait
  fi
}

function get_resource() {
  go get istio.io/test-infra/boskos/cmd/mason_client
  # TODO: Remove once submitted
  # go install istio.io/test-infra/boskos/cmd/mason_client
  local type="${1}"
  local owner="${2}"
  local info_path="${3}"
  local file_log="${4}"

  mason_client \
    --type="${type}" \
    --boskos-url='http://boskos.boskos.svc.cluster.local' \
    --owner="${owner}" \
    --info-save "${info_path}" \
    --kubeconfig-save "${HOME}/.kube/config" > "${file_log}" 2>&1 &
  MASON_CLIENT_PID=$!

  local ready
  local exited

  # Wait up to 10 mn by increment of 10 seconds unit ready or failure
  for _ in {1..60}; do
    grep -q READY "${file_log}" && ready=true || ready=false
    if [[ ${ready} == true ]]; then
      cat "${info_path}"
      local project
      project="$(head -n 1 "${info_path}" | tr -d ':')"
      gcloud config set project "${project}"
      return 0
    fi
    kill -s 0 ${MASON_CLIENT_PID} && exited=false || exited=true
    if [[ ${exited} == true ]]; then
      cat "${file_log}"
      echo "Failed to get a Boskos resource"
      return 1
    fi
    sleep 10
  done
  echo 'failed to get a Boskos resource'
  return 1
}

