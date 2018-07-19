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


#######################################
#                                     #
#             e2e-suite               #
#                                     #
#######################################
KUBE_USER="istio-prow-test-job@istio-testing.iam.gserviceaccount.com"

function setup_cluster() {
  kubectl create clusterrolebinding prow-cluster-admin-binding\
    --clusterrole=cluster-admin\
    --user="${KUBE_USER}"
}

function check_cluster() {
  for i in {1..10}
  do
    status=$(kubectl get namespace || echo "Unreachable")
    [[ ${status} == 'Unreachable' ]] || break
    if [ ${i} -eq 10 ]; then
      echo "Cannot connect to the new cluster"; exit 1
    fi
    sleep 5
  done
}

