#!/bin/bash
# This script generates and copies helm charts within the helm tree of this repo.
# Initial setup

set -o errexit
set -o nounset
set -o pipefail
set -x

# shellcheck disable=SC1091
source "/workspace/gcb_env.sh"


WORK_DIR=$(mktemp -d)
HELM_DIR=$(mktemp -d)

echo WORK_DIR = "$WORK_DIR"
echo HELM_DIR = "$HELM_DIR"

# Helm setup
HELM_BUILD_DIR=${HELM_DIR}/istio-repository
HELM="helm --home $HELM_DIR"

# Copy Istio release files to WORK_DIR
gsutil cp  "gs://${CB_GCS_BUILD_PATH}/istio-${CB_VERSION}-linux.tar.gz" .
tar -zxf "istio-${CB_VERSION}-linux.tar.gz"
mkdir -vp "$WORK_DIR/istio"
cp -R "./istio-${CB_VERSION}/install" "$WORK_DIR/istio/install"

pushd "$WORK_DIR"
    git clone -b master https://github.com/istio-ecosystem/cni.git
popd


# Charts to extract from repos
CHARTS=(
  "${WORK_DIR}/istio/install/kubernetes/helm/istio"
  "${WORK_DIR}/istio/install/kubernetes/helm/istio-remote"
  "${WORK_DIR}/cni/deployments/kubernetes/install/helm/istio-cni"
)

# Prepare helm setup
mkdir -vp "$HELM_DIR"
$HELM init --client-only

# Create a package for each charts and build the repo index.
mkdir -vp "$HELM_BUILD_DIR"
for CHART_PATH in "${CHARTS[@]}"
do
    $HELM package -u "$CHART_PATH" -d "$HELM_BUILD_DIR"
done

$HELM repo index "$HELM_BUILD_DIR"

# Copy output to GCS bucket.
gsutil -qm cp -r "${HELM_BUILD_DIR}/*" "gs://${CB_GCS_BUILD_PATH}/charts/"

# Do the cleanup.
rm -fr "${HELM_DIR}"
rm -fr "${WORK_DIR}"

