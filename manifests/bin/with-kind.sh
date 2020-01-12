#!/bin/bash

# Copyright 2019 Istio Authors
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

set -eux

export ARTIFACTS="${ARTIFACTS:-$(mktemp -d)}"

# Temporary hack
export PATH=${GOPATH}/bin:${PATH}

function cleanup_kind_cluster() {
  if [[ -z "${SKIP_KIND_CLEANUP:-}" ]]; then
    kind export logs --name istio-testing "${ARTIFACTS}/kind" || true
    kind delete cluster --name=istio-testing
  fi
}

function setup_kind_cluster() {
  # Delete any previous e2e KinD cluster
  echo "Deleting previous KinD cluster with name=istio-testing"
  if ! (kind delete cluster --name=istio-testing) > /dev/null; then
    echo "No existing kind cluster with name istio-testing. Continue..."
  fi

  trap cleanup_kind_cluster EXIT

  # Create KinD cluster
  if ! (kind create cluster --name=istio-testing --config test/kind/kind-prow.yaml --loglevel debug --retain --wait 30s); then
    echo "Could not setup KinD environment. Something wrong with KinD setup. Exporting logs."
    exit 1
  fi

  KUBECONFIG="$(kind get kubeconfig-path --name="istio-testing")"
  export KUBECONFIG
}

setup_kind_cluster

"$@"
