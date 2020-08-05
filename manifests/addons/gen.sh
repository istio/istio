#!/usr/bin/env bash

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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

set -eux

# This script sets up the plain text rendered deployments for addons
# See samples/addons/README.md for more information

ADDONS="${WD}/../../samples/addons"
DASHBOARDS="${WD}/../charts/istio-telemetry/grafana/dashboards"
mkdir -p "${ADDONS}"

# Set up kiali
{
helm3 template kiali-server \
  --namespace istio-system \
  --version 1.22.0 \
  --include-crds \
  --set nameOverride=kiali \
  --set fullnameOverride=kiali \
  kiali-server \
  --repo https://kiali.org/helm-charts \
  -f "${WD}/values-kiali.yaml"
} > "${ADDONS}/kiali.yaml"

# Set up prometheus
helm3 template prometheus stable/prometheus \
  --namespace istio-system \
  --version 11.7.0 \
  -f "${WD}/values-prometheus.yaml" \
  > "${ADDONS}/prometheus.yaml"

# Set up grafana
{
  helm3 template grafana stable/grafana \
    --namespace istio-system \
    --version 5.3.5 \
    -f "${WD}/values-grafana.yaml"

  # Set up grafana dashboards. Split into 2 to avoid Kubernetes size limits
  echo -e "\n---\n"
  kubectl create configmap -n istio-system istio-grafana-dashboards \
    --dry-run=client -oyaml \
    --from-file=pilot-dashboard.json="${DASHBOARDS}/pilot-dashboard.json" \
    --from-file=mixer-dashboard.json="${DASHBOARDS}/mixer-dashboard.json" \
    --from-file=istio-performance-dashboard.json="${DASHBOARDS}/istio-performance-dashboard.json"

  echo -e "\n---\n"
  kubectl create configmap -n istio-system istio-services-grafana-dashboards \
    --dry-run=client -oyaml \
    --from-file=istio-workload-dashboard.json="${DASHBOARDS}/istio-workload-dashboard.json" \
    --from-file=istio-service-dashboard.json="${DASHBOARDS}/istio-service-dashboard.json" \
    --from-file=istio-mesh-dashboard.json="${DASHBOARDS}/istio-mesh-dashboard.json"
} > "${ADDONS}/grafana.yaml"
