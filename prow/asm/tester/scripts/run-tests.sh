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

# This script is mainly responsible for setting up the SUT and running the tests.
# The env vars used here are set by the integ-suite-kubetest2.sh script, which
# is the entrypoint for the test jobs run by Prow.

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# shellcheck source=prow/asm/tester/scripts/asm-lib.sh
source "${WD}/asm-lib.sh"
# shellcheck source=prow/asm/tester/scripts/infra-lib.sh
source "${WD}/infra-lib.sh"
# shellcheck source=prow/asm/tester/scripts/revision-lib.sh
source "${WD}/revision-lib.sh"

export BUILD_WITH_CONTAINER=0

# shellcheck disable=SC2034
# holds multiple kubeconfigs for onprem MC
declare -a ONPREM_MC_CONFIGS
# shellcheck disable=SC2034
# hold the configs for baremetal SC
declare -a BAREMETAL_SC_CONFIG
declare BM_ARTIFACTS_PATH
declare BM_HOST_IP

# hold the ENVIRON_PROJECT_ID used for ASM Onprem cluster installation with Hub
declare ENVIRON_PROJECT_ID
# hold the http proxy used for connected to baremetal SC
declare HTTP_PROXY
declare HTTPS_PROXY

echo "Running with CA ${CA}, ${WIP} Workload Identity Pool, ${CONTROL_PLANE} and --vm=${USE_VM} control plane."
# used in telemetry test to decide expected source/destination principal.
export CONTROL_PLANE

if [[ -z "${KUBECONFIG}" ]]; then
  echo "Error: KUBECONFIG env var cannot be empty."
  exit 1
fi
echo "Using ${KUBECONFIG} to connect to the cluster(s)"

if [[ -z "${CLUSTER_TYPE}" ]]; then
  echo "Error: CLUSTER_TYPE env var cannot be empty."
  exit 1
fi
echo "The cluster type is ${CLUSTER_TYPE}"
# only use user-kubeconfig.yaml files on onprem
[[ "${CLUSTER_TYPE}" == "gke-on-prem" ]] && filter_onprem_kubeconfigs
# only use kubeconfig files on baremetal and construct http proxy value
[[ "${CLUSTER_TYPE}" == "bare-metal" ]] && filter_baremetal_kubeconfigs && init_baremetal_http_proxy

if [[ -z "${CLUSTER_TOPOLOGY}" ]]; then
  echo "Error: CLUSTER_TOPOLOGY env var cannot be empty."
  exit 1
fi
echo "The cluster topology is ${CLUSTER_TOPOLOGY}"

if [[ -z "${TEST_TARGET}" ]]; then
  echo "Error: TEST_TARGET env var cannot be empty."
  exit 1
fi
echo "The test target is ${TEST_TARGET}"

# For MULTIPROJECT_MULTICLUSTER topology, firewall rules need to be added to
# allow the clusters talking with each other for security tests.
# See the details in b/175599359 and b/177919868
if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT_MULTICLUSTER" || "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
  gcloud compute --project="${HOST_PROJECT}" firewall-rules create extended-firewall-rule --network=test-network --allow=tcp,udp,icmp --direction=INGRESS
fi

# Get all contexts of the clusters.
CONTEXTSTR=$(kubectl config view -o jsonpath="{range .contexts[*]}{.name}{','}{end}")
CONTEXTSTR="${CONTEXTSTR::-1}"
IFS="," read -r -a CONTEXTS <<< "$CONTEXTSTR"

# Set up the private CA if it's using the gke clusters.
if [[ "${CLUSTER_TYPE}" == "gke" && "${CA}" == "PRIVATECA" ]]; then
  setup_private_ca "${CONTEXTSTR}"
  add_trap "cleanup_private_ca ${CONTEXTSTR}" EXIT SIGKILL SIGTERM SIGQUIT
fi

# If it's using the gke clusters, use one of the projects to hold the images.
if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
  # Use the gcr of the first project to store required images.
  IFS="_" read -r -a VALS <<< "${CONTEXTS[0]}"
  GCR_PROJECT_ID=${VALS[1]}
# Otherwise use the shared GCP project to hold these images.
else
  GCR_PROJECT_ID="${SHARED_GCP_PROJECT}"
fi

# The Makefile passes the path defined in INTEGRATION_TEST_TOPOLOGY_FILE to --istio.test.kube.topology on go test.
export INTEGRATION_TEST_TOPOLOGY_FILE
INTEGRATION_TEST_TOPOLOGY_FILE="${ARTIFACTS}/integration_test_topology.yaml"
if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
  gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${CONTEXTS[@]}"
fi

# exported GCR_PROJECT_ID_1, GCR_PROJECT_ID_2, WIP and CA values are needed
# for security and telemetry test.
export GCR_PROJECT_ID_1=${GCR_PROJECT_ID}
if [[ "${#CONTEXTS[@]}" -gt 1 ]]; then
  IFS="_" read -r -a VALS_2 <<< "${CONTEXTS[1]}"
  export GCR_PROJECT_ID_2=${VALS_2[1]}
else
  export GCR_PROJECT_ID_2="${GCR_PROJECT_ID_1}"
fi

if [[ "${FEATURE_TO_TEST}" == "VPC_SC" ]]; then
  NETWORK_NAME="default"
  if [[ "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
    NETWORK_NAME="test-network"
  fi
  # Create the route as per the user guide in https://docs.google.com/document/d/11yYDxxI-fbbqlpvUYRtJiBmGdY_nIKPJLbssM3YQtKI/edit#heading=h.e2laig460f1d.
  gcloud compute routes create restricted-vip --network="${NETWORK_NAME}" --destination-range=199.36.153.4/30 --next-hop-gateway=default-internet-gateway
  if [[ "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
    # Enable private ip access for VPC_SC
    gcloud compute networks subnets update "test-network-${GCR_PROJECT_ID_1}" \
      --project="${HOST_PROJECT}" \
      --region=us-central1 \
      --enable-private-ip-google-access
    gcloud compute networks subnets update "test-network-${GCR_PROJECT_ID_2}" \
      --project="${HOST_PROJECT}" \
      --region=us-central1 \
      --enable-private-ip-google-access
  fi
fi

# TODO(ruigu): extract the common part of MANAGED and UNMANAGED when MANAGED test is added.
if [[ "${CONTROL_PLANE}" == "UNMANAGED" ]]; then
  echo "Setting up ASM ${CONTROL_PLANE} control plane for test"

  export HUB="gcr.io/${GCR_PROJECT_ID}/asm"
  export TAG="BUILD_ID_${BUILD_ID}"

  if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
    echo "Set permissions to allow the Pods on the GKE clusters to pull images..."
    set_gcp_permissions "${GCR_PROJECT_ID}" "${CONTEXTSTR}"
    add_trap "remove_gcp_permissions ${GCR_PROJECT_ID} ${CONTEXTSTR}" EXIT SIGKILL SIGTERM SIGQUIT
  else
    echo "Set permissions to allow the Pods on the multicloud clusters to pull images..."
    set_multicloud_permissions "${GCR_PROJECT_ID}" "${CONTEXTSTR}"
  fi

  echo "Preparing images..."
  prepare_images
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    # Proxy env needs to be unset to let gcloud command run correctly
    add_trap "unset_http_proxy" EXIT SIGKILL SIGTERM SIGQUIT
  fi
  # Do not clean up the images for multicloud tests, since they are using a
  # shared GCR to store the images, cleaning them up could lead to race
  # conditions for jobs that are running in parallel
  # TODO(chizhg): figure out a way to still clean them up and also avoid the race
  # conditions, potentially using the project rental pool to ensure the isolation.
  if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
    add_trap "cleanup_images" EXIT SIGKILL SIGTERM SIGQUIT
  fi

  if [[ "${WIP}" == "HUB" ]]; then
    echo "Register clusters into the Hub..."
    # Use the first project as the GKE Hub host project
    GKEHUB_PROJECT_ID="${GCR_PROJECT_ID}"
    register_clusters_in_hub "${GKEHUB_PROJECT_ID}" "${CONTEXTS[@]}"
    add_trap "cleanup_hub_setup ${GKEHUB_PROJECT_ID} ${CONTEXTSTR}" EXIT SIGKILL SIGTERM SIGQUIT
  fi

  echo "Building istioctl..."
  build_istioctl

  echo "Installing ASM control plane..."
  if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
    if [ -z "${REVISION_CONFIG_FILE}" ]; then
      install_asm "${CONFIG_DIR}/kpt-pkg" "${CA}" "${WIP}" "" "" "" "${CONTEXTS[@]}"
    else
      install_asm_revisions "${REVISION_CONFIG_FILE}" "${CONFIG_DIR}/kpt-pkg" "${WIP}" "${CONTEXTS[@]}"
    fi
  else
    if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
      export HTTP_PROXY
      export HTTPS_PROXY
      install_asm_on_baremetal
      multicloud::gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}"
      export -n HTTP_PROXY
      export -n HTTPS_PROXY
    else
      install_asm_on_multicloud "${CA}" "${WIP}"
      multicloud::gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}"
    fi
  fi

  if [ -n "${STATIC_VMS}" ]; then
    echo "Setting up GCP VMs to test against"
    VM_CTX="${CONTEXTS[0]}"
    setup_asm_vms "${STATIC_VMS}" "${VM_CTX}" "${VM_DISTRO}" "${IMAGE_PROJECT}"
    static_vm_topology_entry "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${VM_CTX}"
  fi

  echo "Processing kubeconfig files for running the tests..."
  process_kubeconfigs

  # when CLUSTER_TYPE is gke-on-prem, GCR_PROJECT_ID is set to SHARED_GCP_PROJECT istio-prow-build
  # istio-prow-build is not the environ project
  # ENVIRON_PROJECT_ID is the project ID of the environ project where the Onprem cluster is registered
  if [[ "${CLUSTER_TYPE}" == "gke-on-prem" && "${WIP}" == "HUB" ]]; then
    export GCR_PROJECT_ID_1="${ENVIRON_PROJECT_ID}"
  fi
  # When HUB Workload Identity Pool is used in the case of multi projects setup, clusters in different projects
  # will use the same WIP and P4SA of the Hub host project.
  if [[ "${WIP}" == "HUB" ]] && [[ "${TEST_TARGET}" =~ "security" ]]; then
    export GCR_PROJECT_ID_2="${GCR_PROJECT_ID_1}"
  fi

  if [[ "${FEATURE_TO_TEST}" == "USER_AUTH" ]]; then
    add_trap "cleanup_asm_user_auth" EXIT SIGKILL SIGTERM SIGQUIT
    install_asm_user_auth

    kubectl get configmap istio -n istio-system -o yaml
    kubectl get ns --show-labels
    kubectl get pods --all-namespaces

    add_trap "cleanup_dependencies" EXIT SIGKILL SIGTERM SIGQUIT
    download_dependencies

    # TODO(b/182912549): port-forward in go code
    kubectl port-forward service/istio-ingressgateway 8443:443 -n istio-system &
    add_trap "kill -9 $(pgrep -f "kubectl port-forward")" EXIT SIGKILL SIGTERM SIGQUIT
  fi

  # DISABLED_TESTS contains a list of all tests we skip
  # pilot/ tests
  if [[ -n "${DISABLED_TESTS}" ]]; then
    DISABLED_TESTS+="|"
  fi
  DISABLED_TESTS+="TestWait|TestVersion|TestProxyStatus" # UNSUPPORTED: istioctl doesn't work
  DISABLED_TESTS+="|TestAnalysisWritesStatus" # UNSUPPORTED: require custom installation
  # telemetry/ tests
  DISABLED_TESTS+="|TestDashboard" # UNSUPPORTED: Relies on istiod in cluster. TODO: filter out only pilot-dashboard.json
  DISABLED_TESTS+="|TestCustomizeMetrics" # UNKNOWN: b/177606974
  DISABLED_TESTS+="|TestStackdriverAudit" # BROKEN: b/183508169 Disabling Stackdriver Audit tests
  DISABLED_TESTS+="|TestTraffic/virtualservice/shifting.*" # BROKEN: b/184593218
  # security/ tests

  # Skip the subtests that are known to be not working.
  DISABLED_TESTS+="|TestRequestAuthentication/.*/valid-token-forward-remote-jwks" # UNSUPPORTED: relies on custom options
  if [[ "${CLUSTER_TYPE}" == "gke-on-prem" && "${WIP}" == "HUB" ]]; then
    # TODO: Unskip test cases when the same tests in https://b.corp.google.com/issues/174440952 are fixed
    # pilot/ tests
    DISABLED_TESTS+="|TestMirroring/mirror-percent-absent/.*|TestMirroring/mirror-50/.*|TestTraffic/sniffing/.*|TestTraffic/virtualservice/shifting.*|TestMirroring/mirror-10/from_primary-0/.*"
    # telemetry/ tests
    DISABLED_TESTS+="|TestMetrics/telemetry_asm|TestMetricsAudit/telemetry_asm"
    # security/ tests
    DISABLED_TESTS+="|TestAuthorization_WorkloadSelector/From_primary-1/.*|TestAuthorization_NegativeMatch/From_primary-1/.*|TestAuthorization_Conditions/IpA_IpB_IpC_in_primary-0/From_primary-1/.*|TestAuthorization_mTLS/From_primary-1/.*|TestAuthorization_JWT/From_primary-1/.*"
  fi
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    DISABLED_TESTS+="|TestAuthorization_EgressGateway" # UNSUPPORTED: Relies on egress gateway deployed to the cluster.
    DISABLED_TESTS+="|TestStrictMTLS" # UNSUPPORTED: Mesh CA does not support ECDSA
    DISABLED_TESTS+="|TestAuthorization_Custom" # UNSUPPORTED: requires mesh config
    # telemetry/ tests
    DISABLED_TESTS+="|TestStackdriver|TestStackdriverAudit|TestVMTelemetry" # Stackdriver is not enabled with ASM Distro Baremetal installation
  fi

  DISABLED_PACKAGES="/pilot/cni" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/ca_migration" # NOT SUPPORTED in most tests. Has its own target
  DISABLED_PACKAGES+="\|/telemetry/stats/prometheus/nullvm" # UNKNOWN TODO: https://buganizer.corp.google.com/issues/184312874
  DISABLED_PACKAGES+="\|/telemetry/stats/prometheus/wasm" # UNKNOWN TODO: https://buganizer.corp.google.com/issues/184312874

  # TODO: Unskip telemetry stats tests when https://b.corp.google.com/issues/177606974 is fixed
  # stats filter tests are flaky for multiproject
  if [[ "${CLUSTER_TOPOLOGY}" == "MULTIPROJECT" || "${CLUSTER_TOPOLOGY}" == "mp" ]]; then
    DISABLED_TESTS+="|TestStatsFilter|TestTcpMetric|TestWasmStatsFilter|TestWASMTcpMetric"
  fi

  # For security tests, do not run tests that require custom setups.
  export TEST_SELECT="${TEST_SELECT:-}"
  # TODO(nmittler): Remove this once we no longer run the multicluster tests.
  if [[ $TEST_TARGET == "test.integration.multicluster.kube.presubmit" ]]; then
    TEST_SELECT="+multicluster"
  fi
  if [[ $TEST_TARGET == "test.integration.asm.security" ]]; then
    if [[ -z "${TEST_SELECT}" ]]; then
      TEST_SELECT="-customsetup"
    fi
  fi

  export INTEGRATION_TEST_FLAGS="${INTEGRATION_TEST_FLAGS:-}"

  # Don't deploy Istio. Instead just use the pre-installed ASM
  INTEGRATION_TEST_FLAGS+=" --istio.test.kube.deploy=false"

  if [[ "${CLUSTER_TYPE}" != "gke" ]]; then
    if [[ -n "${ASM_REVISION_LABEL}" ]]; then
      INTEGRATION_TEST_FLAGS+=" --istio.test.revision ${ASM_REVISION_LABEL}"
    fi

    if [[ -n "${TEST_IMAGE_PULL_SECRET}" ]]; then
      INTEGRATION_TEST_FLAGS+=" --istio.test.imagePullSecret ${TEST_IMAGE_PULL_SECRET}"
    fi
  fi

  # Don't run VM tests. Echo deployment requires the eastwest gateway
  # which isn't deployed for all configurations.
  if [[ "${USE_VM}" == false ]]; then
    INTEGRATION_TEST_FLAGS+=" --istio.test.skipVM"
  fi

  if [[ -n "${STATIC_VMS}" ]]; then
    # Static real VMs pre-create a namespace
    INTEGRATION_TEST_FLAGS+=" --istio.test.stableNamespaces"
    export DISABLED_PACKAGES+="\|/pilot/endpointslice" # we won't reinstall the CP in endpointslice mode
    # waiting for an oSS change that fixes this test's incompatibility with stableNamespaces
    INTEGRATION_TEST_FLAGS+=" --istio.test.skip=\"TestValidation\""
    # TODO these are the only security tests that excercise VMs. The other tests are written in a way they panic with StaticVMs.
    if [ "${TEST_TARGET}" == "test.integration.asm.security" ]; then
      INTEGRATION_TEST_FLAGS+=" -run=TestReachability\|TestMtlsStrictK8sCA\|TestPassThroughFilterChain"
    fi
  fi

  # Skip the tests that are known to be not working.
  apply_skip_disabled_tests "${DISABLED_TESTS}"
  echo "Running e2e test: ${TEST_TARGET}..."
  export DISABLED_PACKAGES
  export JUNIT_OUT="${ARTIFACTS}/junit1.xml"
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    HTTP_PROXY="${HTTP_PROXY}" BM_ARTIFACTS_PATH="${BM_ARTIFACTS_PATH}" BM_HOST_IP="${BM_HOST_IP}" make "${TEST_TARGET}"
  else
    make "${TEST_TARGET}"
  fi
else
  echo "Setting up ASM ${CONTROL_PLANE} control plane for test"
  export HUB="gcr.io/asm-staging-images/asm-mcp-e2e-test"
  export TAG="BUILD_ID_${BUILD_ID}"
  # needed for telemetry test
  export GCR_PROJECT_ID

  echo "Preparing images for managed control plane..."
  prepare_images_for_managed_control_plane
  add_trap "cleanup_images_for_managed_control_plane" EXIT SIGKILL SIGTERM SIGQUIT

  echo "Building istioctl..."
  build_istioctl

  install_asm_managed_control_plane "${CONTEXTS[@]}"
  for i in "${!CONTEXTS[@]}"; do
    kubectl wait --for=condition=Ready --timeout=2m -n istio-system --all pod --context="${CONTEXTS[$i]}"
  done

  # DISABLED_TESTS contains a list of all tests we skip
  # pilot/ tests
  DISABLED_TESTS="TestWait|TestVersion|TestProxyStatus|TestXdsProxyStatus" # UNSUPPORTED: istioctl doesn't work
  DISABLED_TESTS+="|TestXdsVersion|TestKubeInject" # UNSUPPORTED: b/184572948
  DISABLED_TESTS+="|TestAnalysisWritesStatus" # UNSUPPORTED: require custom installation
  DISABLED_TESTS+="|TestMultiVersionRevision" # UNSUPPORTED: deploys istiod in the cluster, which fails since its using the wrong root cert
  DISABLED_TESTS+="|TestVmOSPost" # BROKEN: temp, pending oss pr
  DISABLED_TESTS+="|TestVMRegistrationLifecycle" # UNSUPPORTED: Attempts to interact with Istiod directly
  DISABLED_TESTS+="|TestValidation|TestWebhook" # BROKEN: b/170404545 currently broken
  DISABLED_TESTS+="|TestAddToAndRemoveFromMesh" # BROKEN: Test current doesn't respect --istio.test.revision
  DISABLED_TESTS+="|TestGateway" # BROKEN: CRDs need to be deployed before Istiod runs. In this case, we install Istiod first, causing failure.
  DISABLED_TESTS+="|TestRevisionedUpgrade" # UNSUPPORTED: OSS Control Plane upgrade is not supported by MCP.
  DISABLED_TESTS+="|TestTraffic/virtualservice/shifting.*" # BROKEN: b/184593218
  # telemetry/ tests
  DISABLED_TESTS+="|TestStackdriverAudit|TestStackdriverHTTPAuditLogging" # UNSUPPORTED: Relies on customized installation of the stackdriver envoyfilter.
  DISABLED_TESTS+="|TestIstioctlMetrics" # UNSUPPORTED: istioctl doesn't work
  DISABLED_TESTS+="|TestBadWasmRemoteLoad|TestWasmStatsFilter|TestWASMTcpMetric" # UNSUPPORTED Relies on enabling WASM during installation.
  DISABLED_TESTS+="|TestDashboard" # UNSUPPORTED: Relies on istiod in cluster. TODO: filter out only pilot-dashboard.json
  DISABLED_TESTS+="|TestProxyTracing|TestClientTracing|TestRateLimiting" # NOT SUPPORTED: requires customized meshConfig setting
  DISABLED_TESTS+="|TestCustomizeMetrics" # NOT SUPPORTED: Replies on customization on the stats envoyFilter
  DISABLED_TESTS+="|TestOutboundTrafficPolicy" # UNSUPPORTED: Relies on egress gateway deployed to the cluster. TODO: filter out only Traffic_Egress
  DISABLED_TESTS+="|TestStackdriverAudit" # BROKEN: b/183508169 Disabling Stackdriver Audit tests
  # security/ tests
  DISABLED_TESTS+="|TestAuthorization_IngressGateway" # UNKNOWN
  DISABLED_TESTS+="|TestAuthorization_EgressGateway" # UNSUPPORTED: Relies on egress gateway deployed to the cluster.
  DISABLED_TESTS+="|TestStrictMTLS" # UNSUPPORTED: Mesh CA does not support ECDSA
  DISABLED_TESTS+="|TestAuthorization_Custom" # UNSUPPORTED: requires mesh config
  # DISABLED_PACKAGES contains a list of all packages we skip
  DISABLED_PACKAGES="/multicluster" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/pilot/cni" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/pilot/revisions" # Attempts to install Istio
  DISABLED_PACKAGES+="\|/pilot/revisioncmd" # NOT SUPPORTED. Customize control plane values.
  DISABLED_PACKAGES+="\|pilot/analysis" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/pilot/endpointslice" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/pilot/mcs" # Unknown (b/183429870)
  DISABLED_PACKAGES+="\|/helm" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/operator" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/telemetry/stackdriver/vm" # NOT SUPPORTED (vm)
  DISABLED_PACKAGES+="\|/telemetry/stats/prometheus/customizemetrics" # NOT SUPPORTED: Replies on customization on the stats envoyFilter
  DISABLED_PACKAGES+="\|/telemetry/stats/prometheus/wasm" # UNSUPPORTED Relies on enabling WASM during installation.
  DISABLED_PACKAGES+="\|/security/chiron" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/file_mounted_certs" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/ca_custom_root" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/filebased_tls_origination" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/mtls_first_party_jwt" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/mtlsk8sca" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/sds_ingress_k8sca" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/sds_tls_origination" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/ca_migration" # NOT SUPPORTED in most tests. Has its own target
  DISABLED_PACKAGES+="\|/security/user_auth" # NOT SUPPORTED

  # MCP doesn't support custom Istio installation
  # TODO(ruigu): Environment flags will be set in the Prow job definition.
  TEST_SELECT="-customsetup"
  export TEST_SELECT

  if [[ "${CLUSTER_TOPOLOGY}" == "SINGLECLUSTER" || "${CLUSTER_TOPOLOGY}" == "sc" ]]; then
    echo "Running integration test with ASM managed control plane and ${CLUSTER_TOPOLOGY} topology"

    DISABLED_TESTS+="|TestStackdriverMonitoring" # NOT NEEDED (duplication): This one uses fake stackdriver. Multi-cluster MCP telemetry tests uses real stackdriver.

    export INTEGRATION_TEST_FLAGS
    apply_skip_disabled_tests "${DISABLED_TESTS}"
    export DISABLED_PACKAGES
    TAG="latest" HUB="gcr.io/istio-testing" \
      make test.integration.asm.mcp \
      INTEGRATION_TEST_FLAGS+="--istio.test.kube.deploy=false \
  --istio.test.revision=asm-managed \
  --istio.test.skipVM=true \
  --istio.test.skip=\"TestRequestAuthentication/.*/valid-token-forward-remote-jwks\"" # UNSUPPORTED: relies on custom options
  elif [[ "${CLUSTER_TOPOLOGY}" == "MULTICLUSTER" || "${CLUSTER_TOPOLOGY}" == "mc" ]]; then
    echo "Running integration test with ASM managed control plane and ${CLUSTER_TOPOLOGY} topology"

    echo "Processing kubeconfig files for running the tests..."
    process_kubeconfigs

    export INTEGRATION_TEST_FLAGS="${INTEGRATION_TEST_FLAGS:-}"
    # TODO(nmittler): Remove this once we no longer run the multicluster tests.
    if [[ $TEST_TARGET == "test.integration.multicluster.kube.presubmit" ]]; then
      TEST_SELECT+=",+multicluster"
    fi

    # ASM MCP requires revision to be asm-managed.
    INTEGRATION_TEST_FLAGS+=" --istio.test.revision=asm-managed"
    # Don't deploy Istio. Instead just use the pre-installed ASM
    INTEGRATION_TEST_FLAGS+=" --istio.test.kube.deploy=false"
    # Don't run VM tests. Echo deployment requires the eastwest gateway
    # which isn't deployed for all configurations.
    INTEGRATION_TEST_FLAGS+=" --istio.test.skipVM"
    # Skip the tests that are known to be not working.
    INTEGRATION_TEST_FLAGS+=" --istio.test.skip=\"TestRequestAuthentication/.*/valid-token-forward-remote-jwks\"" # UNSUPPORTED: relies on custom options

    apply_skip_disabled_tests "${DISABLED_TESTS}"
    echo "Running e2e test: ${TEST_TARGET}..."
    export DISABLED_PACKAGES
    export JUNIT_OUT="${ARTIFACTS}/junit1.xml"
    make "${TEST_TARGET}"
  fi
fi
