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

WD=$(dirname "$0")
WD=$(cd "$WD"; pwd)

# Exit immediately for non zero status
set -e
# Check unset variables
set -u
# Print commands
set -x

# shellcheck source=prow/asm/tester/scripts/libs/asm-lib.sh
source "${WD}/libs/asm-lib.sh"

# shellcheck source=prow/asm/tester/scripts/libs/vm-lib.sh
source "${WD}/libs/vm-lib.sh"

# holds multiple kubeconfigs for Multicloud test environments
declare -a MC_CONFIGS
declare HTTP_PROXY
declare HTTPS_PROXY
# shellcheck disable=SC2034
IFS=':' read -r -a MC_CONFIGS <<< "${KUBECONFIG}"

# construct http proxy value for aws
[[ "${CLUSTER_TYPE}" == "aws" ]] && aws::init

echo "======= env vars ========"
printenv

# Get all contexts of the clusters into an array
IFS="," read -r -a CONTEXTS <<< "${CONTEXT_STR}"

# The Makefile passes the path defined in INTEGRATION_TEST_TOPOLOGY_FILE to --istio.test.kube.topology on go test.
export INTEGRATION_TEST_TOPOLOGY_FILE
INTEGRATION_TEST_TOPOLOGY_FILE="${ARTIFACTS}/integration_test_topology.yaml"
if [[ "${CLUSTER_TYPE}" == "gke" ]]; then
  gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${CONTEXTS[@]}"
else
  multicloud::gen_topology_file "${INTEGRATION_TEST_TOPOLOGY_FILE}"
fi

if [[ "${CONTROL_PLANE}" == "UNMANAGED" ]]; then
  if [ -n "${STATIC_VMS}" ]; then
    echo "Setting up GCP VMs to test against"
    VM_CTX="${CONTEXTS[0]}"
    setup_asm_vms "${STATIC_VMS}" "${VM_CTX}" "${VM_DISTRO}" "${IMAGE_PROJECT}"
    static_vm_topology_entry "${INTEGRATION_TEST_TOPOLOGY_FILE}" "${VM_CTX}"
  fi

  # DISABLED_TESTS contains a list of all tests we skip
  if [[ -n "${DISABLED_TESTS}" ]]; then
    DISABLED_TESTS+="|"
  fi
  # pilot/ tests
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
  DISABLED_TESTS+="|TestAuthorization_IngressGateway" # UNKNOWN
  DISABLED_TESTS+="|TestAuthorization_EgressGateway" # UNSUPPORTED: Relies on egress gateway deployed to the cluster.

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

  if [[ "${CLUSTER_TYPE}" == "aws" ]]; then
    DISABLED_TESTS+="|TestAuthorization_EgressGateway" # UNSUPPORTED: Relies on egress gateway deployed to the cluster.
    DISABLED_TESTS+="|TestStrictMTLS" # UNSUPPORTED: Mesh CA does not support ECDSA
    DISABLED_TESTS+="|TestAuthorization_Custom" # UNSUPPORTED: requires mesh config
    # TODO: unskip ingress tests after b/184990912 is fixed
    DISABLED_TESTS+="|TestAuthorization_IngressGateway|TestIngressRequestAuthentication|TestMtlsGatewaysK8sca|TestTlsGatewaysK8sca|TestSingleTlsGateway*|TestSingleMTLSGateway*|TestTlsGateways|TestMtlsGateways|TestMultiTlsGateway_InvalidSecret|TestMultiMtlsGateway_InvalidSecret"
    DISABLED_TESTS+="|TestAuthorization_Deny|TestAuthorization_TCP|TestAuthorization_Conditions|TestAuthorization_GRPC|TestAuthorization_Audit|TestPassThroughFilterChain"
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
    create_asm_revision_label
    if [[ -n "${ASM_REVISION_LABEL}" ]]; then
      INTEGRATION_TEST_FLAGS+=" --istio.test.revision ${ASM_REVISION_LABEL}"
    fi

    TEST_IMAGE_PULL_SECRET="${ARTIFACTS}/test_image_pull_secret.yaml"
    INTEGRATION_TEST_FLAGS+=" --istio.test.imagePullSecret ${TEST_IMAGE_PULL_SECRET}"
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
    HTTP_PROXY="${HTTP_PROXY}" make "${TEST_TARGET}"
  else
    make "${TEST_TARGET}"
  fi
else
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
  DISABLED_TESTS+="|TestRequestAuthentication" # UNSUPPORTED: https://buganizer.corp.google.com/issues/180418442
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
