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

# shellcheck source=prow/asm/asm-lib.sh
source "${WD}/asm-lib.sh"
# shellcheck source=prow/asm/infra-lib.sh
source "${WD}/infra-lib.sh"

export BUILD_WITH_CONTAINER=0

# CA = CITADEL, MESHCA or PRIVATECA
CA="MESHCA"
# WIP(Workload Identity Pool) = GKE or HUB
WIP="GKE"
# CONTROL_PLANE = UNMANAGED or MANAGED
CONTROL_PLANE="UNMANAGED"
# USE_VM = true or false
USE_VM=false
export USE_VM
# STATIC_VMS = a directory in echo-vm-provisioner/configs
STATIC_VMS=""
export STATIC_VMS
# Makefile target
TEST_TARGET="test.integration.multicluster.kube.presubmit"
# Passed by job config
DISABLED_TESTS=""
# holds multiple kubeconfigs for onprem MC
declare -a ONPREM_MC_CONFIGS
# hold the kubeconfig for baremetal SC
declare -a BAREMETAL_SC_CONFIG

# hold the ENVIRON_PROJECT_ID used for ASM Onprem cluster installation with Hub
declare ENVIRON_PROJECT_ID
# hold the http proxy used for connected to baremetal SC
declare HTTP_PROXY
declare HTTPS_PROXY

while (( "$#" )); do
  case $1 in
    --ca)
      case $2 in
        "CITADEL" | "MESHCA" | "PRIVATECA" )
          CA=$2
          shift 2
          ;;
        *)
          echo "Error: Unsupported CA $2" >&2
          exit 1
          ;;
      esac
      ;;
    --control-plane)
      case $2 in
        "UNMANAGED" | "MANAGED" )
          CONTROL_PLANE=$2
          shift 2
          ;;
        *)
          echo "Error: Unsupported ASM Control Plane $2" >&2
          exit 1
          ;;
      esac
      ;;
    --wip)
      case $2 in
        "GKE" | "HUB" )
          WIP=$2
          shift 2
          ;;
        *)
          echo "Error: Unsupported Workload Identity Pool $2" >&2
          exit 1
          ;;
      esac
      ;;
    --vm)
      USE_VM=true
      shift 1
      ;;
    --static-vms)
      STATIC_VMS=$2
      shift 2
      ;;
    --test)
      TEST_TARGET=$2
      shift 2
      ;;
    --disabled-tests)
      DISABLED_TESTS=$2
      shift 2
      ;;
    *)
      echo "Error: Unsupported input $1" >&2
      exit 1
      ;;
  esac
done

echo "Running with CA ${CA}, ${WIP} Workload Identity Pool, ${CONTROL_PLANE} and --vm=${USE_VM} control plane."
# used in telemetry test to decide expected source/destination principal.
export CONTROL_PLANE

if [[ -z "${KUBECONFIG}" ]]; then
  echo "Error: ${KUBECONFIG} cannot be empty."
  exit 1
fi

echo "Using ${KUBECONFIG} to connect to the cluster(s)"

if [[ -z "${CLUSTER_TYPE}" ]]; then
  echo "Error: ${CLUSTER_TYPE} cannot be empty."
  exit 1
fi
echo "The cluster type is ${CLUSTER_TYPE}"
# only use user-kubeconfig.yaml files on onprem
[[ "${CLUSTER_TYPE}" == "gke-on-prem" ]] && filter_onprem_kubeconfigs
# only use kubeconfig files on baremetal and construct http proxy value
[[ "${CLUSTER_TYPE}" == "bare-metal" ]] && filter_baremetal_kubeconfigs && init_baremetal_http_proxy

if [[ -z "${CLUSTER_TOPOLOGY}" ]]; then
  echo "Error: ${CLUSTER_TOPOLOGY} cannot be empty."
  exit 1
fi
echo "The cluster topology is ${CLUSTER_TOPOLOGY}"

if [[ -z "${TEST_TARGET}" ]]; then
  echo "Error: ${TEST_TARGET} cannot be empty."
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
# Otherwise use the central GCP project to hold these images.
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
    # TODO: remove it if there is a general solution for b/174580152
    set_multicloud_permissions "${GCR_PROJECT_ID}" "${CONTEXTSTR}"
  fi

  echo "Preparing images..."
  prepare_images
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    # Proxy env needs to be unset to let gcloud command run correctly
    add_trap "unset_http_proxy" EXIT SIGKILL SIGTERM SIGQUIT
  fi
  add_trap "cleanup_images" EXIT SIGKILL SIGTERM SIGQUIT

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
    install_asm "${WD}/kpt-pkg" "${CA}" "${WIP}" "${CONTEXTS[@]}"
  else
    if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
      export HTTP_PROXY
      export HTTPS_PROXY
      install_asm_on_baremetal
      multicloud::gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}"
      export -n HTTP_PROXY
      export -n HTTPS_PROXY
    else
      install_asm_on_multicloud "${WD}/infra" "${CA}" "${WIP}"
      multicloud::gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}"
    fi
  fi

  if [ -n "${STATIC_VMS}" ]; then
    echo "Setting up GCP VMs to test against"
    VM_CTX="${CONTEXTS[0]}"
    setup_asm_vms "${STATIC_VMS}" "${VM_CTX}"
    static_vm_topology_entry "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${VM_CTX}"
  fi

  echo "Processing kubeconfig files for running the tests..."
  process_kubeconfigs

  # when CLUSTER_TYPE is gke-on-prem, GCR_PROJECT_ID is set to CENTRAL_GCP_PROJECT istio-prow-build
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
  export CA
  export WIP

  if [[ "${FEATURE_TO_TEST}" == "USER_AUTH" ]]; then
    install_asm_user_auth "${WD}"
    kubectl wait --for=condition=Ready --timeout=2m -n iap --all pod --context="${CONTEXTS[$i]}"
    kubectl get configmap istio -n istio-system -o yaml
    kubectl get ns --show-labels
    kubectl get pods --all-namespaces
    add_trap "cleanup_asm_user_auth ${WD}" EXIT SIGKILL SIGTERM SIGQUIT
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
    # TODO: Fix the ingress connection issue via proxy
    DISABLED_TESTS+="|TestAuthorization_IngressGateway|TestIngressRequestAuthentication|TestPassThroughFilterChain"
    DISABLED_TESTS+="|TestAuthorization_EgressGateway" # UNSUPPORTED: Relies on egress gateway deployed to the cluster.
    DISABLED_TESTS+="|TestStrictMTLS" # UNSUPPORTED: Mesh CA does not support ECDSA
    DISABLED_TESTS+="|TestAuthorization_Custom" # UNSUPPORTED: requires mesh config
  fi

  DISABLED_PACKAGES="/pilot/cni" # NOT SUPPORTED

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
    else
      TEST_SELECT+=",-customsetup"
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
      INTEGRATION_TEST_FLAGS+=" -run=TestReachability\|TestMtlsStrictK8sCA"
    fi
  fi

  # Skip the tests that are known to be not working.
  apply_skip_disabled_tests "${DISABLED_TESTS}"
  echo "Running e2e test: ${TEST_TARGET}..."
  export DISABLED_PACKAGES
  export JUNIT_OUT="${ARTIFACTS}/junit1.xml"
  if [[ "${CLUSTER_TYPE}" == "bare-metal" ]]; then
    HTTP_PROXY="${HTTP_PROXY}" make "${TEST_TARGET}"
  else
    make "${TEST_TARGET}"
  fi
else
  echo "Setting up ASM ${CONTROL_PLANE} control plane for test"
  export HUB="gcr.io/wlhe-cr/asm-mcp-e2e-test"
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
  DISABLED_TESTS="TestWait|TestVersion|TestProxyStatus" # UNSUPPORTED: istioctl doesn't work
  DISABLED_TESTS+="|TestAnalysisWritesStatus" # UNSUPPORTED: require custom installation
  DISABLED_TESTS+="|TestMultiVersionRevision" # UNSUPPORTED: deploys istiod in the cluster, which fails since its using the wrong root cert
  DISABLED_TESTS+="|TestVmOSPost" # BROKEN: temp, pending oss pr
  DISABLED_TESTS+="|TestVMRegistrationLifecycle" # UNSUPPORTED: Attempts to interact with Istiod directly
  DISABLED_TESTS+="|TestValidation|TestWebhook" # BROKEN: b/170404545 currently broken
  DISABLED_TESTS+="|TestAddToAndRemoveFromMesh" # BROKEN: Test current doesn't respect --istio.test.revision
  DISABLED_TESTS+="|TestGateway" # BROKEN: CRDs need to be deployed before Istiod runs. In this case, we install Istiod first, causing failure.
  DISABLED_TESTS+="|TestRevisionedUpgrade" # UNSUPPORTED: OSS Control Plane upgrade is not supported by MCP.
  # telemetry/ tests
  DISABLED_TESTS+="|TestStackdriverAudit|TestStackdriverHTTPAuditLogging" # UNSUPPORTED: Relies on customized installation of the stackdriver envoyfilter.
  DISABLED_TESTS+="|TestIstioctlMetrics" # UNSUPPORTED: istioctl doesn't work
  DISABLED_TESTS+="|TestBadWasmRemoteLoad|TestWasmStatsFilter|TestWASMTcpMetric" # UNSUPPORTED Relies on enabling WASM during installation.
  DISABLED_TESTS+="|TestDashboard" # UNSUPPORTED: Relies on istiod in cluster. TODO: filter out only pilot-dashboard.json
  DISABLED_TESTS+="|TestProxyTracing|TestClientTracing|TestRateLimiting" # NOT SUPPORTED: requires customized meshConfig setting
  DISABLED_TESTS+="|TestCustomizeMetrics" # NOT SUPPORTED: Replies on customization on the stats envoyFilter
  DISABLED_TESTS+="|TestOutboundTrafficPolicy" # UNSUPPORTED: Relies on egress gateway deployed to the cluster. TODO: filter out only Traffic_Egress
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
  DISABLED_PACKAGES+="\|/helm" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/operator" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/telemetry/stackdriver/vm" # NOT SUPPORTED (vm)
  DISABLED_PACKAGES+="\|/security/chiron" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/file_mounted_certs" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/ca_custom_root" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/filebased_tls_origination" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/mtls_first_party_jwt" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/mtlsk8sca" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/sds_ingress_k8sca" # NOT SUPPORTED
  DISABLED_PACKAGES+="\|/security/sds_tls_origination" # NOT SUPPORTED

  if [[ "${CLUSTER_TOPOLOGY}" == "SINGLECLUSTER" || "${CLUSTER_TOPOLOGY}" == "sc" ]]; then
    echo "Running integration test with ASM managed control plane and ${CLUSTER_TOPOLOGY} topology"

    DISABLED_TESTS+="|TestStackdriverMonitoring" # NOT NEEDED (duplication): This one uses fake stackdriver. Multi-cluster MCP telemetry tests uses real stackdriver.

    export DISABLED_PACKAGES
    TAG="latest" HUB="gcr.io/istio-testing" \
      make test.integration.asm.mcp \
      INTEGRATION_TEST_FLAGS="--istio.test.kube.deploy=false \
  --istio.test.revision=asm-managed \
  --istio.test.skipVM=true \
  --istio.test.skip=\"${DISABLED_TESTS}\" \
  --istio.test.skip=\"TestRequestAuthentication/.*/valid-token-forward-remote-jwks\"" # UNSUPPORTED: relies on custom options
  elif [[ "${CLUSTER_TOPOLOGY}" == "MULTICLUSTER" || "${CLUSTER_TOPOLOGY}" == "mc" ]]; then
    echo "Running integration test with ASM managed control plane and ${CLUSTER_TOPOLOGY} topology"

    echo "Processing kubeconfig files for running the tests..."
    process_kubeconfigs

    export CA
    export WIP

    export INTEGRATION_TEST_FLAGS="${INTEGRATION_TEST_FLAGS:-}"
    # For security tests, do not run tests that require custom setups.
    export TEST_SELECT="${TEST_SELECT:-}"
    # TODO(nmittler): Remove this once we no longer run the multicluster tests.
    if [[ $TEST_TARGET == "test.integration.multicluster.kube.presubmit" ]]; then
      TEST_SELECT="+multicluster"
    fi
    if [[ $TEST_TARGET == "test.integration.asm.security" ]]; then
      TEST_SELECT="-customsetup"
    fi

    # ASM MCP requires revision to be asm-managed.
    INTEGRATION_TEST_FLAGS+=" --istio.test.revision=asm-managed"
    # Don't deploy Istio. Instead just use the pre-installed ASM
    INTEGRATION_TEST_FLAGS+=" --istio.test.kube.deploy=false"
    # Don't run VM tests. Echo deployment requires the eastwest gateway
    # which isn't deployed for all configurations.
    INTEGRATION_TEST_FLAGS+=" --istio.test.skipVM"
    # Skip the tests that are known to be not working.
    INTEGRATION_TEST_FLAGS+=" --istio.test.skip=\"${DISABLED_TESTS}\""
    INTEGRATION_TEST_FLAGS+=" --istio.test.skip=\"TestRequestAuthentication/.*/valid-token-forward-remote-jwks\"" # UNSUPPORTED: relies on custom options

    echo "Running e2e test: ${TEST_TARGET}..."
    export DISABLED_PACKAGES
    export JUNIT_OUT="${ARTIFACTS}/junit1.xml"
    make "${TEST_TARGET}"
  fi
fi
