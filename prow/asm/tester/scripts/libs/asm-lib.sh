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

readonly ROOT_CA_ID_PREFIX="asm-test-root-ca"
readonly ROOT_CA_LOC="us-central1"
readonly SUB_CA_ID_PREFIX="asm-test-sub-ca"

# shellcheck disable=SC2034
# holds multiple kubeconfigs for Multicloud test environments
declare -a MC_CONFIGS
# shellcheck disable=SC2034
IFS=':' read -r -a MC_CONFIGS <<< "${KUBECONFIG}"

function buildx-create() {
  export DOCKER_CLI_EXPERIMENTAL=enabled
  if ! docker buildx ls | grep -q container-builder; then
    docker buildx create --driver-opt network=host,image=gcr.io/istio-testing/buildkit:buildx-stable-1 --name container-builder
    # Pre-warm the builder. If it fails, fetch logs, but continue
    docker buildx inspect --bootstrap container-builder || docker logs buildx_buildkit_container-builder0 || true
  fi
  docker buildx use container-builder
}

# Process kubeconfig files to make sure current-context in each file
# is set correctly.
# Depends on env var ${KUBECONFIG}
function process_kubeconfigs() {
  local KUBECONFIGPATHS
  IFS=":" read -r -a KUBECONFIGPATHS <<< "${KUBECONFIG}"
  # Each kubeconfig file should have one and only one cluster context.
  for i in "${!KUBECONFIGPATHS[@]}"; do
    local CONTEXT_STR
    CONTEXT_STR=$(kubectl config view -o jsonpath="{.contexts[0].name}" --kubeconfig="${KUBECONFIGPATHS[$i]}")
    kubectl config use-context "${CONTEXT_STR}" --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done

  for i in "${!KUBECONFIGPATHS[@]}"; do
    kubectl config view --kubeconfig="${KUBECONFIGPATHS[$i]}"
  done
}

# Prepare images required for the e2e test.
# Depends on env var ${HUB} and ${TAG}
function prepare_images() {
  buildx-create
  # Configure Docker to authenticate with Container Registry.
  gcloud auth configure-docker
  # Build images from the current branch and push the images to gcr.
  make dockerx.pushx HUB="${HUB}" TAG="${TAG}" DOCKER_TARGETS="docker.pilot docker.proxyv2 docker.app"

  docker pull gcr.io/asm-staging-images/asm/stackdriver-prometheus-sidecar:e2e-test
  docker tag gcr.io/asm-staging-images/asm/stackdriver-prometheus-sidecar:e2e-test "${HUB}/stackdriver-prometheus-sidecar:${TAG}"
  docker push "${HUB}/stackdriver-prometheus-sidecar:${TAG}"
}

# Prepare images (istiod, proxyv2) required for the managed control plane e2e test.
# Depends on env var ${HUB} and ${TAG}
function prepare_images_for_managed_control_plane() {
  buildx-create
  # Configure Docker to authenticate with Container Registry.
  gcloud auth configure-docker
  # Build images from the current branch and push the images to gcr.
  HUB="${HUB}" TAG="${TAG}" DOCKER_TARGETS="docker.cloudrun docker.proxyv2 docker.app" make dockerx.pushx
}

# Build istioctl in the current branch to install ASM.
function build_istioctl() {
  make istioctl
  cp "$PWD/out/linux_amd64/istioctl" "/usr/local/bin"
}

# Delete temporary images created for the e2e test.
# Depends on env var ${HUB} and ${TAG}
function cleanup_images() {
  gcloud beta container images delete "${HUB}/app:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/pilot:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/proxyv2:${TAG}" --force-delete-tags --quiet
  gcloud beta container images delete "${HUB}/stackdriver-prometheus-sidecar:${TAG}" --force-delete-tags --quiet
}

# Delete temporary images created for the managed control plane e2e test.
# Depends on env var ${HUB} and ${TAG}
function cleanup_images_for_managed_control_plane() {
  gcloud beta container images delete "${HUB}/cloudrun:${TAG}" --force-delete-tags || true
  gcloud beta container images delete "${HUB}/proxyv2:${TAG}" --force-delete-tags || true
}

# Register the clusters into the Hub of the Hub host project.
# Parameters: $1 - Hub host project
#             $2 - array of k8s contexts
function register_clusters_in_hub() {
  local GKEHUB_PROJECT_ID=$1; shift
  local CONTEXTS=("${@}")
  local ENVIRON_PROJECT_NUMBER
  ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${GKEHUB_PROJECT_ID}" --format="value(projectNumber)")

  # Create Hub service account for the Hub host project
  gcloud beta services identity create --service=gkehub.googleapis.com --project="${GKEHUB_PROJECT_ID}"

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    echo "Hub registration for ${CONTEXTS[$i]}"
    local PROJECT_ID=${VALS[1]}
    local CLUSTER_LOCATION=${VALS[2]}
    local CLUSTER_NAME=${VALS[3]}
    # Add IAM binding for Hub SA in the Hub connect project
    gcloud projects add-iam-policy-binding "${PROJECT_ID}" \
      --member "serviceAccount:service-${ENVIRON_PROJECT_NUMBER}@gcp-sa-gkehub.iam.gserviceaccount.com" \
      --role roles/gkehub.serviceAgent
    # This is the user guide for registering clusters within Hub
    # https://cloud.devsite.corp.google.com/service-mesh/docs/register-cluster
    # Verify two ways of Hub registration
    # if the cluster is in the Hub host project
    if [[ "${PROJECT_ID}" == "${GKEHUB_PROJECT_ID}" ]]; then
      gcloud beta container hub memberships register "${CLUSTER_NAME}" --project="${PROJECT_ID}" \
        --gke-cluster="${CLUSTER_LOCATION}"/"${CLUSTER_NAME}" \
        --enable-workload-identity \
        --quiet
    # if the cluster is in the connect project
    else
      gcloud beta container hub memberships register "${CLUSTER_NAME}" --project="${GKEHUB_PROJECT_ID}" \
        --gke-uri=https://container.googleapis.com/v1/projects/"${PROJECT_ID}"/locations/"${CLUSTER_LOCATION}"/clusters/"${CLUSTER_NAME}" \
        --enable-workload-identity \
        --quiet
    fi
  done
  echo "These are the Hub Memberships within Host project ${GKEHUB_PROJECT_ID}"
  gcloud beta container hub memberships list --project="${GKEHUB_PROJECT_ID}"
}

# Revert the operations in register_clusters_in_hub.
# Parameters: $1 - Hub host project
#             $2 - a string of k8s contexts
function cleanup_hub_setup() {
  local GKEHUB_PROJECT_ID=$1
  IFS="," read -r -a CONTEXTS <<< "$2"

  local ENVIRON_PROJECT_NUMBER
  ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${GKEHUB_PROJECT_ID}" --format="value(projectNumber)")
  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    local PROJECT_ID=${VALS[1]}
    local CLUSTER_NAME=${VALS[3]}
    # Remove added IAM binding for Hub SA
    if [[ "${PROJECT_ID}" != "${GKEHUB_PROJECT_ID}" ]]; then
      gcloud projects remove-iam-policy-binding "${PROJECT_ID}" \
        --member "serviceAccount:service-${ENVIRON_PROJECT_NUMBER}@gcp-sa-gkehub.iam.gserviceaccount.com" \
        --role roles/gkehub.serviceAgent
    fi
  done
}

# Setup the private CAs.
# Parameters: $1 - comma-separated string of k8s contexts
function setup_private_ca() {
  IFS="," read -r -a CONTEXTS <<< "$1"

  local ROOT_CA_ID="${ROOT_CA_ID_PREFIX}-${BUILD_ID}"
  # Create root CA in the central project if it does not exist.
  if ! gcloud beta privateca roots list --project "${SHARED_GCP_PROJECT}" --location "${ROOT_CA_LOC}" | grep -q "${ROOT_CA_ID}"; then
    echo "Creating root CA ${ROOT_CA_ID}..."
    gcloud beta privateca roots create "${ROOT_CA_ID}" \
      --location "${ROOT_CA_LOC}" \
      --project "${SHARED_GCP_PROJECT}" \
      --subject "CN=ASM Test Root CA, O=Google LLC" \
      --tier enterprise \
      --reusable-config root-unconstrained \
      --key-algorithm rsa-pkcs1-4096-sha256
  fi

  # Contains temporary files for subordinate CA activation.
  WORKING_DIR=$(mktemp -d)
  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    PROJECT_ID="${VALS[1]}"
    LOCATION="${VALS[2]}"
    CLUSTER="${VALS[3]}"
    local SUBORDINATE_CA_ID="${SUB_CA_ID_PREFIX}-${BUILD_ID}-${CLUSTER}"
    local CSR_FILE="${WORKING_DIR}/${SUBORDINATE_CA_ID}.csr"
    local CERT_FILE="${WORKING_DIR}/${SUBORDINATE_CA_ID}.crt"
    local WORKLOAD_IDENTITY="$PROJECT_ID.svc.id.goog[istio-system/istiod-service-account]"
    if ! gcloud beta privateca subordinates list --location "${LOCATION}" --project "${PROJECT_ID}" | grep -q "${SUBORDINATE_CA_ID}"; then
      echo "Creating subordinate CA ${SUBORDINATE_CA_ID}..."
      gcloud beta privateca subordinates create "${SUBORDINATE_CA_ID}" \
        --location "${LOCATION}" \
        --project "${PROJECT_ID}" \
        --subject "CN=ASM Test Subordinate CA, O=Google LLC" \
        --tier devops \
        --reusable-config "subordinate-mtls-pathlen-0" \
        --key-algorithm rsa-pkcs1-2048-sha256 \
        --create-csr \
        --csr-output-file "${CSR_FILE}"

      CERT_ID="cert-${SUBORDINATE_CA_ID}"
      echo "Signing subordinate CA certificate ${CERT_ID}.."
      gcloud beta privateca certificates create "${CERT_ID}" \
        --issuer "${ROOT_CA_ID}" \
        --issuer-location "${ROOT_CA_LOC}" \
        --project "${SHARED_GCP_PROJECT}" \
        --csr "${CSR_FILE}" \
        --cert-output-file "${CERT_FILE}" \
        --validity "P3Y" # Change this as needed - 3Y is the default for subordinate CAs.

      echo "Activating subordinate CA.."
      gcloud beta privateca subordinates activate "${SUBORDINATE_CA_ID}" \
        --location "${LOCATION}" \
        --project "${PROJECT_ID}" \
        --pem-chain "${CERT_FILE}"
    fi
    gcloud beta privateca subordinates add-iam-policy-binding "${SUBORDINATE_CA_ID}" \
      --location "${LOCATION}" \
      --project "${PROJECT_ID}" \
      --member "serviceAccount:$WORKLOAD_IDENTITY" \
      --role "roles/privateca.certificateManager" \
      --quiet
  done
}

# Cleanup the private CAs.
# Parameters: $1 - a string of k8s contexts
function cleanup_private_ca() {
  # Install the uuid tool to generate uuid for the curl command to purge CA in
  # the end.
  apt-get update
  apt-get install uuid -y

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    PROJECT_ID="${VALS[1]}"
    LOCATION="${VALS[2]}"
    CLUSTER="${VALS[3]}"

    local SUBORDINATE_CA_ID="${SUB_CA_ID_PREFIX}-${BUILD_ID}-${CLUSTER}"
    if gcloud beta privateca subordinates list --project "${PROJECT_ID}" --location "${LOCATION}" | grep -q "${SUBORDINATE_CA_ID}"; then
      echo "Purging subordinate CA $SUBORDINATE_CA_ID.."
      purge-ca "subordinates" "${SUBORDINATE_CA_ID}" "${LOCATION}" "${PROJECT_ID}"
    fi
  done
  local ROOT_CA_ID="${ROOT_CA_ID_PREFIX}-${BUILD_ID}"
  if gcloud beta privateca roots list --project "${SHARED_GCP_PROJECT}" --location "${ROOT_CA_LOC}" | grep -q "${ROOT_CA_ID}"; then
    echo "Purging root CA $ROOT_CA_ID.."
    purge-ca "roots" "${ROOT_CA_ID}" "${ROOT_CA_LOC}" "${SHARED_GCP_PROJECT}"
  fi
}

# Purge the CA.
# Parameters: $1 - the command group (roots|subordinates)
#             $2 - the full CA resource name
#             $3 - location
#             $4 - project ID
function purge-ca() {
  echo "Purging CA '$2'."
  local certs
  # shellcheck disable=SC2207
  certs=($(gcloud beta privateca certificates list \
    --issuer "$2" \
    --format 'table(name.scope(), revocation_details)' \
    --location="$3" --project="$4" | grep -Po '([^\s]+)(?=\s+ACTIVE)' || true))

  echo "> Revoking ${#certs[@]} certificates.." >&2

  for cert in "${certs[@]}"; do
    gcloud beta privateca certificates revoke --certificate "$cert" --quiet
  done

  if gcloud beta privateca "$1" describe "$2" --format "value(state)" --location="$3" --project="$4" | grep -q "ENABLED" ; then
    echo "> Disabling and deleting CA.."
    gcloud beta privateca "$1" disable "$2" --location="$3" --project="$4" --quiet
    # As suggested in
    # https://buganizer.corp.google.com/issues/179162450#comment10, delete root
    # CA with the curl command instead of calling `gcloud beta privateca roots
    # delete`
    if [[ "$4" == "${SHARED_GCP_PROJECT}" ]]; then
      curl \
        -H "Authorization: Bearer $(gcloud auth print-access-token)" \
        -H "Content-Type: application/json" \
        -X POST \
        -d '{ "deletePendingDuration": "0s" }' \
        "https://privateca.googleapis.com/v1beta1/projects/$4/locations/$3/certificateAuthorities/$2:scheduleDelete?requestId=$(uuid)"
    else
      gcloud beta privateca "$1" delete "$2" --location="$3" --project="$4" --quiet
    fi
  fi
}

# Install ASM managed control plane.
# Parameters: $1 - array of k8s contexts
# Depends on env var ${HUB} and ${TAG}
function install_asm_managed_control_plane() {
  local CONTEXTS=("${@}")
  # For installation, we'll use the corresponding master branch Scriptaro for ASM branch master-asm.
  git clone --branch master https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages.git
  cp anthos-service-mesh-packages/scripts/asm-installer/install_asm .
  # ASM MCP Prow job connects to staging meshconfig API.
  # The reason is currently, we also use this job to test/alert our staging ADS proxy.
  sed -i 's/meshconfig\.googleapis\.com/staging-meshconfig.sandbox.googleapis.com/g' install_asm
  chmod +x install_asm

  for i in "${!CONTEXTS[@]}"; do
    IFS="_" read -r -a VALS <<< "${CONTEXTS[$i]}"
    local PROJECT_ID="${VALS[1]}"
    local LOCATION="${VALS[2]}"
    local CLUSTER_NAME="${VALS[3]}"
    # Use the first project as the environ project
    if [[ $i == 0 ]]; then
      ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
      echo "Environ project ID: ${PROJECT_ID}, project number: ${ENVIRON_PROJECT_NUMBER}"
    fi
    TMPDIR=$(mktemp -d)


    if [[ "${FEATURE_TO_TEST}" == "ADDON" ]]; then
      # Enable test for addon <-> MCP workload communication
      export ISTIO_TEST_EXTRA_REVISIONS=default
      # Allow auto detection of the CA, dynamically choosing Citadel or Mesh CA based on if an existing
      # Istio cluster is installed.
      cat <<EOF | kubectl --context="${CONTEXTS[$i]}" apply -f - -n istio-system
apiVersion: v1
kind: ConfigMap
metadata:
  name: asm-options
  namespace: istio-system
data:
  ASM_OPTS: |
    CA=check
EOF
      # TODO this needs to be in addon-migrate. But ordering here is tricky, we need to disable it before we apply.
      # Likely the end result will be that we re-apply the new CRDs when user migrates, but have the old CRDs
      # when MCP is "shadow" launched?
      kubectl --context="${CONTEXTS[$i]}" scale deployment istio-operator --replicas=0 -n istio-operator
      while "$(kubectl --context="${CONTEXTS[$i]}" get pods -n istio-operator -oname)" != ""; do
        echo "waiting for pods to terminate"
        sleep 1
      done
      # This will be manual action by user. Technically they can co-exist, but the old Istiod will hold onto the
      # leader election lock, which breaks Ingress support.
      kubectl --context="${CONTEXTS[$i]}" scale deployment istiod-istio-1611 --replicas=0 -n istio-system

      # TODO(b/184940170) install_asm should setup CRDs
      # Normally we set it in Cloudrun, but because istio-system exists that step is skipped
      kubectl apply --context="${CONTEXTS[$i]}" -f manifests/charts/base/files/gen-istio-cluster.yaml \
        --record=false --overwrite=false --force-conflicts=true --server-side
    fi

    # _CI_ASM_PKG_LOCATION _CI_ASM_IMAGE_LOCATION are required for unreleased Scriptaro (master and staging branch).
    # Managed control plane installation will use _CI_ASM_PKG_LOCATION, _CI_ASM_IMAGE_LOCATION for Gateway.
    # For sidecar proxy and Istiod, _CI_CLOUDRUN_IMAGE_HUB and _CI_CLOUDRUN_IMAGE_TAG are used.
    # Currently, we only test with mesh CA.
    _CI_ASM_PKG_LOCATION="asm-staging-images" _CI_ASM_IMAGE_LOCATION="${HUB}" _CI_ASM_IMAGE_TAG="${TAG}" _CI_ASM_KPT_BRANCH="master" _CI_CLOUDRUN_IMAGE_HUB="${HUB}/cloudrun" _CI_CLOUDRUN_IMAGE_TAG="${TAG}" "./install_asm" \
      --mode install \
      --project_id "${PROJECT_ID}" \
      --cluster_location "${LOCATION}" \
      --cluster_name "${CLUSTER_NAME}" \
      --managed \
      --enable_cluster_labels \
      --enable_namespace_creation \
      --output_dir "${TMPDIR}" \
      --ca "mesh_ca" \
      --verbose

    # Enable access logging to help with debugging tests
    cat <<EOF | kubectl apply --context="${CONTEXTS[$i]}" -f -
apiVersion: v1
data:
  mesh: |-
    accessLogFile: /dev/stdout
kind: ConfigMap
metadata:
  name: asm
  namespace: istio-system
EOF

    if [[ "${FEATURE_TO_TEST}" == "ADDON" ]]; then
      # Run the addon migrate script. This sets up the gateway, so do not install it again
      tools/packaging/knative/migrate-addon.sh -y --context "${CONTEXTS[$i]}"
    else
      kubectl apply -f tools/packaging/knative/gateway/injected-gateway.yaml -n istio-system --context="${CONTEXTS[$i]}"
    fi
  done

  for i in "${!CONTEXTS[@]}"; do
    for j in "${!CONTEXTS[@]}"; do
      if [[ "$i" != "$j" ]]; then
        IFS="_" read -r -a VALS <<< "${CONTEXTS[$j]}"
        local PROJECT_J=${VALS[1]}
        local LOCATION_J=${VALS[2]}
        local CLUSTER_J=${VALS[3]}
        # TODO(ruigu): Ideally, the istioctl here should be the same what Scriptaro used above.
        # However, we don't have a clear way of figuring that out.
        # For now, similar to ASM unmanaged, use the locally built istioctl.
        istioctl x create-remote-secret --context="${CONTEXTS[$j]}" --name="${CLUSTER_J}" > "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret
        kubectl apply -f "${PROJECT_J}"_"${LOCATION_J}"_"${CLUSTER_J}".secret --context="${CONTEXTS[$i]}"
      fi
    done
  done
}


# Outputs YAML to the given file, in the structure of []cluster.Config to inform the test framework of details about
# each cluster under test. cluster.Config is defined in pkg/test/framework/components/cluster/factory.go.
# Parameters: $1 - path to the file to append to
#             $2 - array of k8s contexts
# Depends on env var ${KUBECONFIG}
function gen_topology_file() {
  local file="$1"; shift
  local contexts=("${@}")

  local kubeconfpaths
  IFS=":" read -r -a kubeconfpaths <<< "${KUBECONFIG}"

  for i in "${!contexts[@]}"; do
    IFS="_" read -r -a VALS <<< "${contexts[$i]}"
    local PROJECT_ID=${VALS[1]}
    local CLUSTER_LOCATION=${VALS[2]}
    local CLUSTER_NAME=${VALS[3]}
    cat <<EOF >> "${file}"
- clusterName: "cn-${PROJECT_ID}-${CLUSTER_LOCATION}-${CLUSTER_NAME}"
  kind: Kubernetes
  meta:
    kubeconfig: "${kubeconfpaths[i]}"
EOF
    # Disable using simulated Pod-based "VMs" when testing real VMs
    if [ -n "${STATIC_VMS}" ] || "${GCE_VMS}"; then
      echo '    fakeVM: false' >> "${file}"
    fi
  done
}

# Install ASM on the clusters.
# Parameters: $1 - WIP: GKE or HUB
# Depends on env var ${HUB} and ${TAG}
# TODO(gzip) remove this function once b/176177944 is fixed
function install_asm_on_multicloud() {
  local WIP="$1"; shift
  local MESH_ID="test-mesh"

  export HERCULES_CLI_LAB="atl_shared"
  USER=${USER:-prowuser}
  export USER

  for i in "${!MC_CONFIGS[@]}"; do
    install_certs "${MC_CONFIGS[$i]}"

    echo "----------Installing ASM----------"
    if [[ "${WIP}" == "HUB" ]]; then
      local IDENTITY_PROVIDER
      local IDENTITY
      local HUB_MEMBERSHIP_ID
      IDENTITY_PROVIDER="$(kubectl --kubeconfig="${MC_CONFIGS[$i]}" --context=cluster get memberships membership -o=json | jq .spec.identity_provider)"
      IDENTITY="$(echo "${IDENTITY_PROVIDER}" | sed 's/^\"https:\/\/gkehub.googleapis.com\/projects\/\(.*\)\/locations\/global\/memberships\/\(.*\)\"$/\1 \2/g')"
      read -r ENVIRON_PROJECT_ID HUB_MEMBERSHIP_ID <<EOF
${IDENTITY}
EOF
      local ENVIRON_PROJECT_NUMBER
      ENVIRON_PROJECT_NUMBER=$(gcloud projects describe "${ENVIRON_PROJECT_ID}" --format="value(projectNumber)")
      local PROJECT_ID="${ENVIRON_PROJECT_ID}"
      local CLUSTER_NAME="${HUB_MEMBERSHIP_ID}"
      local CLUSTER_LOCATION="us-central1-a"
      local MESH_ID="proj-${ENVIRON_PROJECT_NUMBER}"

      kpt pkg get https://github.com/GoogleCloudPlatform/anthos-service-mesh-packages.git/asm@master tmp
      kpt cfg set tmp gcloud.compute.network "network${i}"
      kpt cfg set tmp gcloud.core.project "${PROJECT_ID}"
      kpt cfg set tmp gcloud.project.environProjectNumber "${ENVIRON_PROJECT_NUMBER}"
      kpt cfg set tmp gcloud.container.cluster "${CLUSTER_NAME}"
      kpt cfg set tmp gcloud.compute.location "${CLUSTER_LOCATION}"
      kpt cfg set tmp anthos.servicemesh.rev "${ASM_REVISION_LABEL}"
      kpt cfg set tmp anthos.servicemesh.tag "${TAG}"
      kpt cfg set tmp anthos.servicemesh.hub "${HUB}"
      kpt cfg set tmp anthos.servicemesh.hubMembershipID "${HUB_MEMBERSHIP_ID}"
      kpt cfg set tmp gcloud.project.environProjectID "${ENVIRON_PROJECT_ID}"

      echo "----------Istio Operator YAML and Hub Overlay YAML----------"
      cat "tmp/istio/istio-operator.yaml"
      cat "tmp/istio/options/hub-meshca.yaml"

      echo "----------Generating expansion gw YAML----------"
      samples/multicluster/gen-eastwest-gateway.sh \
        --mesh "${MESH_ID}" \
        --cluster "${CLUSTER_NAME}" \
        --revision "${ASM_REVISION_LABEL}" \
        --network "network${i}" > "tmp/eastwest-gateway.yaml"
      cat "tmp/eastwest-gateway.yaml"
      istioctl install -y --kubeconfig="${MC_CONFIGS[$i]}" --context=cluster -f "tmp/istio/istio-operator.yaml" -f "tmp/istio/options/hub-meshca.yaml" -f "tmp/eastwest-gateway.yaml"
    else
      cat <<EOF | istioctl install -y --kubeconfig="${MC_CONFIGS[$i]}" -f -
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: asm-multicloud
  revision: ${ASM_REVISION_LABEL}
  hub: ${HUB}
  tag: ${TAG}
  values:
    global:
      meshID: ${MESH_ID}
      multiCluster:
        clusterName: cluster${i}
      network: network${i}
EOF
      # eastwest gateway is not needed for single cluster installation
      if [[ "${CLUSTER_TOPOLOGY}" != "sc" ]]; then
        install_expansion_gw "${MESH_ID}" "cluster${i}" "network${i}" "${ASM_REVISION_LABEL}" "${HUB}" "${TAG}" "${MC_CONFIGS[$i]}"
      fi
    fi
    # set default network for the cluster, allow detecting network of non-injected pods
    kubectl --kubeconfig="${MC_CONFIGS[$i]}" label namespace istio-system topology.istio.io/network="network${i}"
    expose_services "${MC_CONFIGS[$i]}"
    configure_validating_webhook "${ASM_REVISION_LABEL}" "${MC_CONFIGS[$i]}"

    if [[ "${CLUSTER_TYPE}" == "gke-on-prem" ]]; then
      onprem::configure_external_ip "${MC_CONFIGS[$i]}"
    fi

  done

  configure_remote_secrets
}

# Outputs YAML to the given file, in the structure of []cluster.Config to inform the test framework of details about
# each cluster under test. cluster.Config is defined in pkg/test/framework/components/cluster/factory.go.
# Parameters: $1 - path to the file to append to
# Depends on env var ${KUBECONFIG} and that ASM is already installed (istio-system is present)
function multicloud::gen_topology_file() {
  local TOPO_FILE="$1"; shift
  local KUBECONFIGPATHS
  IFS=":" read -r -a KUBECONFIGPATHS <<< "${KUBECONFIG}"

  if [[ "${CLUSTER_TYPE}" == "bare-metal" || "${CLUSTER_TYPE}" == "aws" ]]; then
    export HTTP_PROXY="${MC_HTTP_PROXY}"
  fi

  # Each kubeconfig file should have one and only one cluster context.
  for i in "${!KUBECONFIGPATHS[@]}"; do
    local CLUSTER_NAME
    CLUSTER_NAME=$(kubectl -n istio-system get pod -l app=istiod -o json --kubeconfig="${KUBECONFIGPATHS[$i]}" | \
      jq -r '.items[0].spec.containers[0].env[] | select(.name == "CLUSTER_ID") | .value')

    cat <<EOF >> "${TOPO_FILE}"
- clusterName: "${CLUSTER_NAME}"
  kind: Kubernetes
  meta:
    kubeconfig: "${KUBECONFIGPATHS[$i]}"
EOF
    echo "  network: network${i}" >> "${TOPO_FILE}"
  done

  if [[ "${CLUSTER_TYPE}" == "bare-metal" || "${CLUSTER_TYPE}" == "aws" ]]; then
    export -n HTTP_PROXY
  fi
}

# Exports variable ASM_REVISION_LABEL with the value being a function of istioctl client version.
function create_asm_revision_label() {
  local ISTIO_CLIENT_TAG
  ISTIO_CLIENT_TAG=$(istioctl version --remote=false -o json | jq -r '.clientVersion.tag')
  IFS='-'; read -r -a tag_tokens <<< "${ISTIO_CLIENT_TAG}"
  unset IFS
  ASM_REVISION_LABEL="asm-${tag_tokens[0]}-${tag_tokens[1]}"
  ASM_REVISION_LABEL=${ASM_REVISION_LABEL//.}
  export ASM_REVISION_LABEL
}

# Creates ca certs on the cluster
# $1    kubeconfig
function install_certs() {
  kubectl create secret generic cacerts -n istio-system \
    --kubeconfig="$1" \
    --from-file=samples/certs/ca-cert.pem \
    --from-file=samples/certs/ca-key.pem \
    --from-file=samples/certs/root-cert.pem \
    --from-file=samples/certs/cert-chain.pem
}

# Creates remote secrets for each cluster pair for all the clusters under test
function configure_remote_secrets() {
  for i in "${!MC_CONFIGS[@]}"; do
    for j in "${!MC_CONFIGS[@]}"; do
      if [[ "$i" != "$j" ]]; then
        istioctl x create-remote-secret \
          --kubeconfig="${MC_CONFIGS[$j]}" \
          --name="secret-${j}" \
        | kubectl apply --kubeconfig="${MC_CONFIGS[$i]}" -f -
      fi
    done
  done
}

# on-prem specific fucntion to configure external ips for the gateways
# Parameters:
# $1    kubeconfig
function onprem::configure_external_ip() {
  local HERC_ENV_ID
  HERC_ENV_ID=$(echo "$1" | rev | cut -d '/' -f 2 | rev)
  local INGRESS_ID=\"lb-test-ip\"
  local EXPANSION_ID=\"expansion-ip\"
  local INGRESS_IP

  echo "Installing herc CLI..."
  gsutil cp "gs://anthos-hercules-public-artifacts/herc/latest/herc" "/usr/local/bin/" && chmod 755 "/usr/local/bin/herc"

  INGRESS_IP=$(herc getEnvironment "${HERC_ENV_ID}" -o json | \
    jq -r ".environment.resources.vcenter_server.datacenter.networks.fe.ip_addresses.${INGRESS_ID}.ip_address")

  # Request additional external IP for expansion gw
  local HERC_PARENT
  HERC_PARENT=$(herc getEnvironment "${HERC_ENV_ID}" | \
    grep "name: environments.*lb-test-ip$" | awk -F' ' '{print $2}' | sed 's/\/ips\/lb-test-ip//')
  local EXPANSION_IP
  EXPANSION_IP=$(herc getEnvironment "${HERC_ENV_ID}" -o json | \
    jq -r ".environment.resources.vcenter_server.datacenter.networks.fe.ip_addresses.${EXPANSION_ID}.ip_address")
  if [[ -z "${EXPANSION_IP}" || "${EXPANSION_IP}" == "null" ]]; then
    echo "Requesting herc for expansion IP"
    herc allocateIPs --parent "${HERC_PARENT}" -f "${CONFIG_DIR}/herc/expansion-ip.yaml"
    EXPANSION_IP=$(herc getEnvironment "${HERC_ENV_ID}" -o json | \
      jq -r ".environment.resources.vcenter_server.datacenter.networks.fe.ip_addresses.${EXPANSION_ID}.ip_address")
  else
    echo "Using ${EXPANSION_IP} as the expansion IP"
  fi

  # Inject the external IPs for GWs
  echo "----------Configuring external IP for ingress gw----------"
  kubectl patch svc istio-ingressgateway -n istio-system \
    --type='json' -p '[{"op": "add", "path": "/spec/loadBalancerIP", "value": "'"${INGRESS_IP}"'"}]' \
    --kubeconfig="$1"
  echo "----------Configuring external IP for expansion gw----------"
  kubectl patch svc istio-eastwestgateway -n istio-system \
    --type='json' -p '[{"op": "add", "path": "/spec/loadBalancerIP", "value": "'"${EXPANSION_IP}"'"}]' \
    --kubeconfig="$1"
}

# Installs expansion gw
# Parameters:
# $1    mesh id
# $2    cluster
# $3    network
# $4    revision
# $5    hub
# $6    tag
# $7    kubeconfig
function install_expansion_gw() {
  echo "----------Installing expansion gw----------"
  samples/multicluster/gen-eastwest-gateway.sh \
    --mesh "$1" \
    --cluster "$2" \
    --network "$3" \
    --revision "$4" \
  | istioctl install -y -f - \
    --set hub="$5" \
    --set tag="$6" \
    --kubeconfig="$7"
}

# Exposes service in istio-system ns
# Parameters:
# $1    kubeconfig
function expose_services() {
  echo "----------Exposing Services----------"
  kubectl apply -n istio-system -f samples/multicluster/expose-services.yaml --kubeconfig="$1"
}

# Configures validating webhook for istiod
# Parameters:
# $1    revision
# $2    kubeconfig
function configure_validating_webhook() {
  echo "----------Configuring validating webhook----------"
  cat <<EOF | kubectl apply --kubeconfig="$2" -f -
apiVersion: v1
kind: Service
metadata:
 name: istiod
 namespace: istio-system
 labels:
   istio.io/rev: ${1}
   app: istiod
   istio: pilot
   release: istio
spec:
 ports:
   - port: 15010
     name: grpc-xds # plaintext
     protocol: TCP
   - port: 15012
     name: https-dns # mTLS with k8s-signed cert
     protocol: TCP
   - port: 443
     name: https-webhook # validation and injection
     targetPort: 15017
     protocol: TCP
   - port: 15014
     name: http-monitoring # prometheus stats
     protocol: TCP
 selector:
   app: istiod
   istio.io/rev: ${1}
EOF
}

# Creates virtual machines, registers them with the cluster and install the test echo app.
# Parameters: $1 - the name of a directory in echo-vm-provisioner describing how to setup the VMs.
#             $2 - the context of the cluster VMs will connect to
function setup_asm_vms() {
  local VM_DIR="${CONFIG_DIR}/echo-vm-provisioner/$1"
  local CONTEXT=$2
  local VM_DISTRO="${3:-debian-10}"
  local IMAGE_PROJECT="${4:-debian-cloud}"

  IFS="_" read -r -a VALS <<< "${CONTEXT}"
  local PROJECT_ID=${VALS[1]}
  local CLUSTER_LOCATION=${VALS[2]}
  local CLUSTER_NAME=${VALS[3]}

  local PROJECT_NUMBER
  PROJECT_NUMBER=$(gcloud projects describe "${PROJECT_ID}" --format="value(projectNumber)")
  local REVISION
  REVISION="$(kubectl --context="${CONTEXT}" -n istio-system get service istio-eastwestgateway -ojsonpath='{.metadata.labels.istio\.io/rev}')"

  export ISTIO_OUT="$PWD/out"
  setup_static_vms "${CONTEXT}" "${CLUSTER_NAME}" "${CLUSTER_LOCATION}" "${PROJECT_NUMBER}" "${REVISION}" "${VM_DIR}" "${VM_DISTRO}" "${IMAGE_PROJECT}"
}

function install_asm_on_proxied_clusters() {
  local MESH_ID="test-mesh"
  for i in "${!MC_CONFIGS[@]}"; do
    install_certs "${MC_CONFIGS[$i]}"
    echo "----------Installing ASM----------"
    cat <<EOF | istioctl install -y --kubeconfig="${MC_CONFIGS[$i]}" -f -
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  profile: asm-multicloud
  revision: ${ASM_REVISION_LABEL}
  hub: ${HUB}
  tag: ${TAG}
  values:
    global:
      meshID: ${MESH_ID}
      multiCluster:
        clusterName: cluster${i}
      network: network${i}
EOF
    configure_validating_webhook "${ASM_REVISION_LABEL}" "${MC_CONFIGS[$i]}"
  done
}
