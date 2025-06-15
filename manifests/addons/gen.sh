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
DASHBOARDS="${WD}/dashboards"
mkdir -p "${ADDONS}"
TMP=$(mktemp -d)
LOKI_VERSION=${LOKI_VERSION:-"6.30.1"}
GRAFANA_VERSION=${GRAFANA_VERSION:-"9.2.2"}

# Set up kiali
{
helm3 template kiali-server \
  --namespace istio-system \
  --version 2.11.0 \
  --set deployment.image_version=v2.11 \
  --include-crds \
  kiali-server \
  --repo https://kiali.org/helm-charts \
  -f "${WD}/values-kiali.yaml"
} > "${ADDONS}/kiali.yaml"

# Set up prometheus
helm3 template prometheus prometheus \
  --namespace istio-system \
  --version 27.20.0 \
  --repo https://prometheus-community.github.io/helm-charts \
  -f "${WD}/values-prometheus.yaml" \
  > "${ADDONS}/prometheus.yaml"

function compressDashboard() {
  < "${DASHBOARDS}/$1" jq -c  > "${TMP}/$1"
}

# Set up grafana
{
  # Generate all dynamic dashboards
  (
    pushd "${DASHBOARDS}" > /dev/null
    jb install
    for file in *.libsonnet; do
      dashboard="${file%.*}"
      jsonnet -J vendor -J lib "${file}" > "${dashboard}-dashboard.gen.json"
    done
  )
  helm3 template grafana grafana \
    --namespace istio-system \
    --version "${GRAFANA_VERSION}" \
    --repo https://grafana.github.io/helm-charts \
    -f "${WD}/values-grafana.yaml"

  # Set up grafana dashboards. Split into 2 and compress to single line json to avoid Kubernetes size limits
  compressDashboard "pilot-dashboard.gen.json"
  compressDashboard "istio-performance-dashboard.json"
  compressDashboard "istio-workload-dashboard.json"
  compressDashboard "istio-service-dashboard.json"
  compressDashboard "istio-mesh-dashboard.gen.json"
  compressDashboard "istio-extension-dashboard.json"
  compressDashboard "ztunnel-dashboard.gen.json"
  echo -e "\n---\n"
  kubectl create configmap -n istio-system istio-grafana-dashboards \
    --dry-run=client -oyaml \
    --from-file=pilot-dashboard.json="${TMP}/pilot-dashboard.gen.json" \
    --from-file=ztunnel-dashboard.json="${TMP}/ztunnel-dashboard.gen.json" \
    --from-file=istio-performance-dashboard.json="${TMP}/istio-performance-dashboard.json"

  echo -e "\n---\n"
  kubectl create configmap -n istio-system istio-services-grafana-dashboards \
    --dry-run=client -oyaml \
    --from-file=istio-workload-dashboard.json="${TMP}/istio-workload-dashboard.json" \
    --from-file=istio-service-dashboard.json="${TMP}/istio-service-dashboard.json" \
    --from-file=istio-mesh-dashboard.json="${TMP}/istio-mesh-dashboard.gen.json" \
    --from-file=istio-extension-dashboard.json="${TMP}/istio-extension-dashboard.json"
} > "${ADDONS}/grafana.yaml"

# Set up loki
{
  helm3 template loki loki \
    --namespace istio-system \
    --version "${LOKI_VERSION}" \
    --repo https://grafana.github.io/helm-charts \
    -f "${WD}/values-loki.yaml"
} > "${ADDONS}/loki.yaml"

# Test that the dashboard links are using UIDs instead of paths
if [[ -f "${DASHBOARDS}/test_dashboard_links.sh" ]]; then
  echo "Testing dashboard links..."
  chmod +x "${DASHBOARDS}/test_dashboard_links.sh"
  "${DASHBOARDS}/test_dashboard_links.sh"
fi
