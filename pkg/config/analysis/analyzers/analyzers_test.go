// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package analyzers

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/annotations"
	"istio.io/istio/pkg/config/analysis/analyzers/authz"
	"istio.io/istio/pkg/config/analysis/analyzers/conditions"
	"istio.io/istio/pkg/config/analysis/analyzers/deployment"
	"istio.io/istio/pkg/config/analysis/analyzers/deprecation"
	"istio.io/istio/pkg/config/analysis/analyzers/destinationrule"
	"istio.io/istio/pkg/config/analysis/analyzers/envoyfilter"
	"istio.io/istio/pkg/config/analysis/analyzers/externalcontrolplane"
	"istio.io/istio/pkg/config/analysis/analyzers/gateway"
	"istio.io/istio/pkg/config/analysis/analyzers/injection"
	"istio.io/istio/pkg/config/analysis/analyzers/k8sgateway"
	"istio.io/istio/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/pkg/config/analysis/analyzers/multicluster"
	schemaValidation "istio.io/istio/pkg/config/analysis/analyzers/schema"
	"istio.io/istio/pkg/config/analysis/analyzers/service"
	"istio.io/istio/pkg/config/analysis/analyzers/serviceentry"
	"istio.io/istio/pkg/config/analysis/analyzers/sidecar"
	"istio.io/istio/pkg/config/analysis/analyzers/telemetry"
	"istio.io/istio/pkg/config/analysis/analyzers/virtualservice"
	"istio.io/istio/pkg/config/analysis/analyzers/webhook"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/util/sets"
)

type message struct {
	messageType *diag.MessageType
	origin      string
}

type testCase struct {
	name             string
	inputFiles       []string
	meshConfigFile   string // Optional
	meshNetworksFile string // Optional
	analyzer         analysis.Analyzer
	expected         []message
	skipAll          bool
}

// Some notes on setting up tests for Analyzers:
// * The resources in the input files don't necessarily need to be completely defined, just defined enough for the analyzer being tested.
// * Please keep this list sorted alphabetically by the pkg.name of the analyzer for convenience
// * Expected messages are in the format {msg.ValidationMessageType, "<ResourceKind>/<Namespace>/<ResourceName>"}.
//   - Note that if Namespace is omitted in the input YAML, it will be skipped here.
var testGrid = []testCase{
	{
		name: "misannoted",
		inputFiles: []string{
			"testdata/misannotated.yaml",
		},
		analyzer: &annotations.K8sAnalyzer{},
		expected: []message{
			{msg.UnknownAnnotation, "Service httpbin"},
			{msg.InvalidAnnotation, "Pod invalid-annotations"},
			{msg.MisplacedAnnotation, "Pod grafana-test"},
			{msg.MisplacedAnnotation, "Deployment fortio-deploy"},
			{msg.MisplacedAnnotation, "Namespace staging"},
			{msg.DeprecatedAnnotation, "Deployment fortio-deploy"},
		},
	},
	{
		name: "alpha",
		inputFiles: []string{
			"testdata/misannotated.yaml",
		},
		analyzer: &maturity.AlphaAnalyzer{},
		expected: []message{
			{msg.AlphaAnnotation, "Deployment fortio-deploy"},
			{msg.AlphaAnnotation, "Pod invalid-annotations"},
			{msg.AlphaAnnotation, "Pod invalid-annotations"},
			{msg.AlphaAnnotation, "Service httpbin"},
			{msg.AlphaAnnotation, "Pod anno-not-set-by-default"},
			{msg.AlphaAnnotation, "Pod anno-not-set-by-default"},
			{msg.AlphaAnnotation, "Pod anno-not-set-by-default"},
			{msg.AlphaAnnotation, "Pod anno-not-set-by-default"},
		},
		skipAll: true,
	},
	{
		name:       "deprecation",
		inputFiles: []string{"testdata/deprecation.yaml"},
		analyzer:   &deprecation.FieldAnalyzer{},
		expected: []message{
			{msg.Deprecated, "VirtualService foo/productpage"},
			{msg.Deprecated, "Sidecar default/no-selector"},
		},
	},
	{
		name:       "externalControlPlaneMissingWebhooks",
		inputFiles: []string{"testdata/externalcontrolplane-missing-urls.yaml"},
		analyzer:   &externalcontrolplane.ExternalControlPlaneAnalyzer{},
		expected: []message{
			{msg.InvalidExternalControlPlaneConfig, "MutatingWebhookConfiguration istio-sidecar-injector-external-istiod"},
			{msg.InvalidExternalControlPlaneConfig, "ValidatingWebhookConfiguration istio-validator-external-istiod"},
		},
	},
	{
		name:       "externalControlPlaneMissingWebhooks",
		inputFiles: []string{"testdata/externalcontrolplane-missing-urls-custom-ns.yaml"},
		analyzer:   &externalcontrolplane.ExternalControlPlaneAnalyzer{},
		expected: []message{
			{msg.InvalidExternalControlPlaneConfig, "MutatingWebhookConfiguration istio-sidecar-injector-custom-ns"},
			{msg.InvalidExternalControlPlaneConfig, "ValidatingWebhookConfiguration istio-validator-custom-ns"},
		},
	},
	{
		name:       "externalControlPlaneUsingIpAddresses",
		inputFiles: []string{"testdata/externalcontrolplane-using-ip-addr.yaml"},
		analyzer:   &externalcontrolplane.ExternalControlPlaneAnalyzer{},
		expected: []message{
			{msg.ExternalControlPlaneAddressIsNotAHostname, "MutatingWebhookConfiguration istio-sidecar-injector-external-istiod"},
		},
	},
	{
		name:       "externalControlPlaneValidWebhooks",
		inputFiles: []string{"testdata/externalcontrolplane-valid-urls.yaml"},
		analyzer:   &externalcontrolplane.ExternalControlPlaneAnalyzer{},
		expected:   []message{
			// no messages, this test case verifies no false positives
		},
	},
	{
		name:       "externalControlPlaneValidWebhooks",
		inputFiles: []string{"testdata/externalcontrolplane-valid-urls-custom-ns.yaml"},
		analyzer:   &externalcontrolplane.ExternalControlPlaneAnalyzer{},
		expected:   []message{
			// no messages, this test case verifies no false positives
		},
	},
	{
		name:       "gatewayNoWorkload",
		inputFiles: []string{"testdata/gateway-no-workload.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "Gateway httpbin-gateway"},
		},
	},
	{
		name:       "gatewayBadPort",
		inputFiles: []string{"testdata/gateway-no-port.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected: []message{
			{msg.GatewayPortNotDefinedOnService, "Gateway httpbin-gateway"},
		},
	},
	{
		name:       "gatewayCorrectPort",
		inputFiles: []string{"testdata/gateway-correct-port.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected:   []message{
			// no messages, this test case verifies no false positives
		},
	},
	{
		name:       "gatewayCustomIngressGateway",
		inputFiles: []string{"testdata/gateway-custom-ingressgateway.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected:   []message{
			// no messages, this test case verifies no false positives
		},
	},
	{
		name:       "gatewayCustomIngressGatewayBadPort",
		inputFiles: []string{"testdata/gateway-custom-ingressgateway-badport.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected: []message{
			{msg.GatewayPortNotDefinedOnService, "Gateway httpbin-gateway"},
		},
	},
	{
		name:       "gatewayCustomIngressGatewayBadPortWithoutTarget",
		inputFiles: []string{"testdata/gateway-custom-ingressgateway-badport-notarget.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected: []message{
			{msg.GatewayPortNotDefinedOnService, "Gateway httpbin-gateway"},
		},
	},
	{
		name:       "gatewayCustomIngressGatewayTranslation",
		inputFiles: []string{"testdata/gateway-custom-ingressgateway-translation.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected:   []message{
			// no messages, this test case verifies no false positives
		},
	},
	{
		name:       "gatewayServiceMatchPod",
		inputFiles: []string{"testdata/gateway-custom-ingressgateway-svcselector.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected: []message{
			{msg.GatewayPortNotDefinedOnService, "Gateway httpbin8002-gateway"},
		},
	},
	{
		name:       "gatewaySecret",
		inputFiles: []string{"testdata/gateway-secrets.yaml"},
		analyzer:   &gateway.SecretAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "Gateway defaultgateway-bogusCredentialName"},
			{msg.ReferencedResourceNotFound, "Gateway customgateway-wrongnamespace"},
			{msg.ReferencedResourceNotFound, "Gateway bogusgateway"},
		},
	},
	{
		name:       "gatewaySecretValidations",
		inputFiles: []string{"testdata/gateway-secrets-validation.yaml"},
		analyzer:   &gateway.SecretAnalyzer{},
		expected: []message{
			{msg.InvalidGatewayCredential, "Gateway defaultgateway-invalid-keys"},
			{msg.InvalidGatewayCredential, "Gateway defaultgateway-missing-keys"},
			{msg.InvalidGatewayCredential, "Gateway defaultgateway-invalid-cert"},
			{msg.InvalidGatewayCredential, "Gateway defaultgateway-expired"},
		},
	},
	{
		name:       "conflicting gateways detect",
		inputFiles: []string{"testdata/conflicting-gateways.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected: []message{
			{msg.ConflictingGateways, "Gateway alpha"},
			{msg.ConflictingGateways, "Gateway beta"},
		},
	},
	{
		name:       "gateways with different port are non-conflicting",
		inputFiles: []string{"testdata/gateway-different-port.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected:   []message{
			// no conflict expected, verify that no false-positive conflict is returned
		},
	},
	{
		name:       "conflicting gateways with multiple port declarations",
		inputFiles: []string{"testdata/conflicting-gateways-multiple-ports.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected: []message{
			{msg.ConflictingGateways, "Gateway alpha"},
			{msg.ConflictingGateways, "Gateway beta"},
		},
	},
	{
		name:       "conflicting gateways detect by sub selector",
		inputFiles: []string{"testdata/conflicting-gateways-subSelector.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected: []message{
			{msg.ConflictingGateways, "Gateway alpha"},
			{msg.ConflictingGateways, "Gateway beta"},
		},
	},
	{
		name:       "conflicting gateways detect: no port",
		inputFiles: []string{"testdata/conflicting-gateways-invalid-port.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected:   []message{},
	},
	{
		name:       "istioInjection",
		inputFiles: []string{"testdata/injection.yaml"},
		analyzer:   &injection.Analyzer{},
		expected: []message{
			{msg.NamespaceNotInjected, "Namespace bar"},
			{msg.PodMissingProxy, "Pod default/noninjectedpod"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace busted"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace multi-ns-1"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace multi-ns-2"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace multi-ns-3"},
		},
	},
	{
		name: "istioInjectionEnableNamespacesByDefault",
		inputFiles: []string{
			"testdata/injection.yaml",
			"testdata/common/sidecar-injector-enabled-nsbydefault.yaml",
		},
		analyzer: &injection.Analyzer{},
		expected: []message{
			{msg.NamespaceInjectionEnabledByDefault, "Namespace bar"},
			{msg.PodMissingProxy, "Pod default/noninjectedpod"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace busted"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace multi-ns-1"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace multi-ns-2"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace multi-ns-3"},
		},
	},
	{
		name: "istioInjectionProxyImageMismatch",
		inputFiles: []string{
			"testdata/injection-with-mismatched-sidecar.yaml",
			"testdata/common/sidecar-injector-configmap.yaml",
		},
		analyzer: &injection.ImageAnalyzer{},
		expected: []message{
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace"},
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace-native"},
		},
	},
	{
		name: "injectionImageDistroless",
		inputFiles: []string{
			"testdata/injection-image-distroless.yaml",
			"testdata/common/sidecar-injector-configmap.yaml",
		},
		meshConfigFile: "testdata/common/meshconfig.yaml",
		analyzer:       &injection.ImageAnalyzer{},
		expected: []message{
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace"},
		},
	},
	{
		name: "injectionImageDistrolessNoMeshConfig",
		inputFiles: []string{
			"testdata/injection-image-distroless-no-meshconfig.yaml",
			"testdata/common/sidecar-injector-configmap.yaml",
		},
		analyzer: &injection.ImageAnalyzer{},
		expected: []message{},
	},
	{
		name: "istioInjectionProxyImageMismatchAbsolute",
		inputFiles: []string{
			"testdata/injection-with-mismatched-sidecar.yaml",
			"testdata/sidecar-injector-configmap-absolute-override.yaml",
		},
		analyzer: &injection.ImageAnalyzer{},
		expected: []message{
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace"},
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace-native"},
		},
	},
	{
		name: "istioInjectionProxyImageMismatchWithRevisionCanary",
		inputFiles: []string{
			"testdata/injection-with-mismatched-sidecar.yaml",
			"testdata/sidecar-injector-configmap-absolute-override.yaml",
			"testdata/sidecar-injector-configmap-with-revision-canary.yaml",
		},
		analyzer: &injection.ImageAnalyzer{},
		expected: []message{
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace"},
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace enabled-namespace-native"},
			{msg.PodsIstioProxyImageMismatchInNamespace, "Namespace revision-namespace"},
		},
	},
	{
		name:       "portNameNotFollowConvention",
		inputFiles: []string{"testdata/service-no-port-name.yaml"},
		analyzer:   &service.PortNameAnalyzer{},
		expected: []message{
			{msg.PortNameIsNotUnderNamingConvention, "Service my-namespace1/my-service1"},
			{msg.PortNameIsNotUnderNamingConvention, "Service my-namespace1/my-service1"},
			{msg.PortNameIsNotUnderNamingConvention, "Service my-namespace2/my-service2"},
		},
	},
	{
		name:       "namedPort",
		inputFiles: []string{"testdata/service-port-name.yaml"},
		analyzer:   &service.PortNameAnalyzer{},
		expected:   []message{},
	},
	{
		name:       "unnamedPortInSystemNamespace",
		inputFiles: []string{"testdata/service-no-port-name-system-namespace.yaml"},
		analyzer:   &service.PortNameAnalyzer{},
		expected:   []message{},
	},
	{
		name:       "sidecarDefaultSelector",
		inputFiles: []string{"testdata/sidecar-default-selector.yaml"},
		analyzer:   &sidecar.SelectorAnalyzer{},
		expected: []message{
			{msg.MultipleSidecarsWithoutWorkloadSelectors, "Sidecar ns2/has-conflict-2"},
			{msg.MultipleSidecarsWithoutWorkloadSelectors, "Sidecar ns2/has-conflict-1"},
			{msg.IneffectivePolicy, "Sidecar ns-ambient/namespace-scoped"},
			{msg.IneffectivePolicy, "Sidecar ns-ambient/pod-scoped"},
		},
	},
	{
		name:       "sidecarSelector",
		inputFiles: []string{"testdata/sidecar-selector.yaml"},
		analyzer:   &sidecar.SelectorAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "Sidecar default/maps-to-nonexistent"},
			{msg.ReferencedResourceNotFound, "Sidecar other/maps-to-different-ns"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar default/dupe-1"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar default/dupe-2"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar default/overlap-1"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar default/overlap-2"},
		},
	},
	{
		name:       "virtualServiceConflictingMeshGatewayHosts",
		inputFiles: []string{"testdata/virtualservice_conflictingmeshgatewayhosts.yaml"},
		analyzer:   &virtualservice.ConflictingMeshGatewayHostsAnalyzer{},
		expected: []message{
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService team3/ratings"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService team4/ratings"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService foo/ratings"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService bar/ratings"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService foo/productpage"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService foo/bogus-productpage"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService team2/ratings"},
		},
	},
	{
		name:       "virtualServiceConflictingMeshGatewayHostsWithExportTo",
		inputFiles: []string{"testdata/virtualservice_conflictingmeshgatewayhosts_with_exportto.yaml"},
		analyzer:   &virtualservice.ConflictingMeshGatewayHostsAnalyzer{},
		expected: []message{
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService foo/productpage"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService bar/productpage"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService foo/productpage-c"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService bar/productpage-c"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService foo/productpage-d"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService bar/productpage-d"},
		},
	},
	{
		name:       "virtualServiceDestinationHosts",
		inputFiles: []string{"testdata/virtualservice_destinationhosts.yaml"},
		analyzer:   &virtualservice.DestinationHostAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-bogushost"},
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-bogushost"},
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-bookinfo-other"},
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-mirror-bogushost"},
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-bogusport"},
			{msg.VirtualServiceDestinationPortSelectorRequired, "VirtualService default/reviews-2port-missing"},
			{msg.ReferencedResourceNotFound, "VirtualService istio-system/cross-namespace-details"},
			{msg.ReferencedResourceNotFound, "VirtualService hello/hello-export-to-bogus"},
		},
	},
	{
		name:       "virtualServiceDestinationRules",
		inputFiles: []string{"testdata/virtualservice_destinationrules.yaml"},
		analyzer:   &virtualservice.DestinationRuleAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-bogussubset"},
			{msg.ReferencedResourceNotFound, "VirtualService default/reviews-mirror-bogussubset"},
		},
	},
	{
		name:       "virtualServiceGateways",
		inputFiles: []string{"testdata/virtualservice_gateways.yaml"},
		analyzer:   &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService httpbin-bogus"},

			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/cross-test"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService httpbin-bogus"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService httpbin"},
		},
	},
	{
		name:       "virtualServiceJWTClaimRoute",
		inputFiles: []string{"testdata/virtualservice_jwtclaimroute.yaml"},
		analyzer:   &virtualservice.JWTClaimRouteAnalyzer{},
		expected: []message{
			{msg.JwtClaimBasedRoutingWithoutRequestAuthN, "VirtualService foo"},
		},
	},
	{
		name:       "virtualServiceInternalGatewayRef",
		inputFiles: []string{"testdata/virtualservice_internal_gateway_ref.yaml"},
		analyzer:   &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.ReferencedInternalGateway, "VirtualService httpbin"},
		},
	},
	{
		name:       "serviceMultipleDeployments",
		inputFiles: []string{"testdata/deployment-multi-service.yaml"},
		analyzer:   &deployment.ServiceAssociationAnalyzer{},
		expected: []message{
			{msg.DeploymentAssociatedToMultipleServices, "Deployment bookinfo/multiple-svc-multiple-prot"},
			{msg.DeploymentAssociatedToMultipleServices, "Deployment bookinfo/multiple-without-port"},
			{msg.DeploymentConflictingPorts, "Deployment bookinfo/conflicting-ports"},
		},
	},
	{
		name:       "deploymentMultiServicesInDifferentNamespace",
		inputFiles: []string{"testdata/deployment-multi-service-different-ns.yaml"},
		analyzer:   &deployment.ServiceAssociationAnalyzer{},
		expected:   []message{},
	},
	{
		name:       "serviceWithNoSelector",
		inputFiles: []string{"testdata/deployment-service-no-selector.yaml"},
		analyzer:   &deployment.ServiceAssociationAnalyzer{},
		expected:   []message{},
	},
	{
		name: "unknown service registry in mesh networks",
		inputFiles: []string{
			"testdata/multicluster-unknown-serviceregistry.yaml",
		},
		meshNetworksFile: "testdata/common/meshnetworks.yaml",
		analyzer:         &multicluster.MeshNetworksAnalyzer{},
		expected: []message{
			{msg.UnknownMeshNetworksServiceRegistry, "MeshNetworks istio-system/meshnetworks"},
			{msg.UnknownMeshNetworksServiceRegistry, "MeshNetworks istio-system/meshnetworks"},
		},
	},
	{
		name: "authorizationpolicies",
		inputFiles: []string{
			"testdata/authorizationpolicies.yaml",
		},
		analyzer: &authz.AuthorizationPoliciesAnalyzer{},
		expected: []message{
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy istio-system/meshwide-httpbin-v1"},
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy httpbin-empty/httpbin-empty-namespace-wide"},
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy httpbin/httpbin-nopods"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin/httpbin-bogus-ns"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin/httpbin-bogus-ns"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin/httpbin-bogus-not-ns"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin/httpbin-bogus-not-ns"},
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy test-ambient/no-workload"},
		},
	},
	{
		name: "destinationrule with no cacert, simple at destinationlevel",
		inputFiles: []string{
			"testdata/destinationrule-simple-destination.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationDestinationLevel, "DestinationRule db-tls"},
		},
	},
	{
		name: "destinationrule with no cacert, mutual at destinationlevel",
		inputFiles: []string{
			"testdata/destinationrule-mutual-destination.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationDestinationLevel, "DestinationRule db-mtls"},
		},
	},
	{
		name: "destinationrule with no cacert, simple at portlevel",
		inputFiles: []string{
			"testdata/destinationrule-simple-port.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationPortLevel, "DestinationRule db-tls"},
		},
	},
	{
		name: "destinationrule with credentialname, simple at destinationlevel, no workloadSelector",
		inputFiles: []string{
			"testdata/destinationrule-simple-destination-credentialname.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationDestinationLevel, "DestinationRule db-tls"},
		},
	},
	{
		name: "destinationrule with credentialname, simple at destinationlevel, workloadSelector",
		inputFiles: []string{
			"testdata/destinationrule-simple-destination-credentialname-selector.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{},
	},
	{
		name: "destinationrule with credentialname, simple at portlevel, no workloadSelector",
		inputFiles: []string{
			"testdata/destinationrule-simple-port-credentialname.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationPortLevel, "DestinationRule db-tls"},
		},
	},
	{
		name: "destinationrule with credentialname, simple at portlevel, workloadSelector",
		inputFiles: []string{
			"testdata/destinationrule-simple-port-credentialname-selector.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{},
	},
	{
		name: "destinationrule with no cacert, mutual at portlevel",
		inputFiles: []string{
			"testdata/destinationrule-mutual-port.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationPortLevel, "DestinationRule db-mtls"},
		},
	},
	{
		name: "destinationrule with no cacert, mutual at destinationlevel and simple at port level",
		inputFiles: []string{
			"testdata/destinationrule-compound-simple-mutual.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationDestinationLevel, "DestinationRule db-mtls"},
			{msg.NoServerCertificateVerificationPortLevel, "DestinationRule db-mtls"},
		},
	},
	{
		name: "destinationrule with no cacert, simple at destinationlevel and mutual at port level",
		inputFiles: []string{
			"testdata/destinationrule-compound-mutual-simple.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{
			{msg.NoServerCertificateVerificationPortLevel, "DestinationRule db-mtls"},
			{msg.NoServerCertificateVerificationDestinationLevel, "DestinationRule db-mtls"},
		},
	},
	{
		name: "destinationrule with both cacerts",
		inputFiles: []string{
			"testdata/destinationrule-with-ca.yaml",
		},
		analyzer: &destinationrule.CaCertificateAnalyzer{},
		expected: []message{},
	},
	{
		name: "dupmatches",
		inputFiles: []string{
			"testdata/virtualservice_dupmatches.yaml",
			"testdata/virtualservice_overlappingmatches.yaml",
		},
		analyzer: schemaValidation.CollectionValidationAnalyzer(collections.VirtualService),
		expected: []message{
			{msg.VirtualServiceUnreachableRule, "VirtualService duplicate-match"},
			{msg.VirtualServiceUnreachableRule, "VirtualService foo/sample-foo-cluster01"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService almost-duplicate-match"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService duplicate-match"},

			{msg.VirtualServiceUnreachableRule, "VirtualService duplicate-tcp-match"},
			{msg.VirtualServiceUnreachableRule, "VirtualService duplicate-empty-tcp"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService almost-duplicate-tcp-match"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService duplicate-tcp-match"},

			{msg.VirtualServiceUnreachableRule, "VirtualService none/tls-routing"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService none/tls-routing-almostmatch"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService none/tls-routing"},

			{msg.VirtualServiceIneffectiveMatch, "VirtualService non-method-get"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService overlapping-in-single-match"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService overlapping-in-two-matches"},
			{msg.VirtualServiceIneffectiveMatch, "VirtualService overlapping-mathes-with-different-methods"},
		},
	},
	{
		name: "host defined in virtualservice not found in the gateway",
		inputFiles: []string{
			"testdata/virtualservice_host_not_found_gateway.yaml",
		},
		analyzer: &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-02-test-01"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-02-test-02"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-02-test-03"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-03-test-04"},
		},
	},
	{
		name: "host defined in virtualservice not found in the gateway(beta version)",
		inputFiles: []string{
			"testdata/virtualservice_host_not_found_gateway_beta.yaml",
		},
		analyzer: &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-02-test-01"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-02-test-02"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-02-test-03"},
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-03-test-04"},
		},
	},
	{
		name: "host defined in virtualservice not found in the gateway with ns",
		inputFiles: []string{
			"testdata/virtualservice_host_not_found_gateway_with_ns_prefix.yaml",
		},
		analyzer: &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-01-test-01"},
		},
	},
	{
		name: "host defined in virtualservice not found in the gateway with ns(beta version)",
		inputFiles: []string{
			"testdata/virtualservice_host_not_found_gateway_with_ns_prefix_beta.yaml",
		},
		analyzer: &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.VirtualServiceHostNotFoundInGateway, "VirtualService default/testing-service-01-test-01"},
		},
	},
	{
		name: "missing Addresses and Protocol in Service Entry",
		inputFiles: []string{
			"testdata/serviceentry-missing-addresses-protocol.yaml",
		},
		analyzer: &serviceentry.ProtocolAddressesAnalyzer{},
		expected: []message{
			{msg.ServiceEntryAddressesRequired, "ServiceEntry default/service-entry-test-03"},
			{msg.ServiceEntryAddressesRequired, "ServiceEntry default/service-entry-test-04"},
			{msg.ServiceEntryAddressesRequired, "ServiceEntry default/service-entry-test-07"},
		},
	},
	{
		name: "missing Addresses and Protocol in Service Entry with ISTIO_META_DNS_AUTO_ALLOCATE enabled",
		inputFiles: []string{
			"testdata/serviceentry-missing-addresses-protocol.yaml",
		},
		meshConfigFile: "testdata/serviceentry-missing-addresses-protocol-mesh-cfg.yaml",
		analyzer:       &serviceentry.ProtocolAddressesAnalyzer{},
		expected:       []message{},
	},
	{
		name: "certificate duplication in Gateway",
		inputFiles: []string{
			"testdata/gateway-duplicate-certificate.yaml",
		},
		analyzer: &gateway.CertificateAnalyzer{},
		expected: []message{
			{msg.GatewayDuplicateCertificate, "Gateway istio-system/gateway-01-test-01"},
			{msg.GatewayDuplicateCertificate, "Gateway istio-system/gateway-02-test-01"},
			{msg.GatewayDuplicateCertificate, "Gateway istio-system/gateway-01-test-02"},
			{msg.GatewayDuplicateCertificate, "Gateway default/gateway-01-test-03"},
		},
	},
	{
		name: "webook",
		inputFiles: []string{
			"testdata/webhook.yaml",
		},
		analyzer: &webhook.Analyzer{},
		expected: []message{
			{msg.InvalidWebhook, "MutatingWebhookConfiguration istio-sidecar-injector-missing-overlap"},
			{msg.InvalidWebhook, "MutatingWebhookConfiguration istio-sidecar-injector-missing-overlap"},
			{msg.InvalidWebhook, "MutatingWebhookConfiguration istio-sidecar-injector-overlap"},
		},
	},
	{
		name: "Route Rule no effect on Ingress",
		inputFiles: []string{
			"testdata/virtualservice_route_rule_no_effects_ingress.yaml",
		},
		analyzer: &virtualservice.DestinationHostAnalyzer{},
		expected: []message{
			{msg.IngressRouteRulesNotAffected, "VirtualService default/testing-service-01-test-01"},
		},
	},
	{
		name: "Application Pod SecurityContext with UID 1337",
		inputFiles: []string{
			"testdata/pod-sec-uid.yaml",
		},
		analyzer: &deployment.ApplicationUIDAnalyzer{},
		expected: []message{
			{msg.InvalidApplicationUID, "Pod pod-sec-uid"},
		},
	},
	{
		name: "Application Container SecurityContext with UID 1337",
		inputFiles: []string{
			"testdata/pod-con-sec-uid.yaml",
		},
		analyzer: &deployment.ApplicationUIDAnalyzer{},
		expected: []message{
			{msg.InvalidApplicationUID, "Pod con-sec-uid"},
		},
	},
	{
		name: "Deployment Pod SecurityContext with UID 1337",
		inputFiles: []string{
			"testdata/deployment-pod-sec-uid.yaml",
		},
		analyzer: &deployment.ApplicationUIDAnalyzer{},
		expected: []message{
			{msg.InvalidApplicationUID, "Deployment deploy-pod-sec-uid"},
		},
	},
	{
		name: "Deployment Container SecurityContext with UID 1337",
		inputFiles: []string{
			"testdata/deployment-con-sec-uid.yaml",
		},
		analyzer: &deployment.ApplicationUIDAnalyzer{},
		expected: []message{
			{msg.InvalidApplicationUID, "Deployment deploy-con-sec-uid"},
		},
	},
	{
		name: "Detect `image: auto` in non-injected pods",
		inputFiles: []string{
			"testdata/image-auto.yaml",
		},
		analyzer: &injection.ImageAutoAnalyzer{},
		expected: []message{
			{msg.ImageAutoWithoutInjectionWarning, "Deployment not-injected/non-injected-gateway-deployment"},
			{msg.ImageAutoWithoutInjectionError, "Pod default/injected-pod"},
		},
	},
	{
		name:       "ExternalNameServiceTypeInvalidPortName",
		inputFiles: []string{"testdata/incorrect-port-name-external-name-service-type.yaml"},
		analyzer:   &service.PortNameAnalyzer{},
		expected: []message{
			{msg.ExternalNameServiceTypeInvalidPortName, "Service nginx-ns/nginx"},
			{msg.ExternalNameServiceTypeInvalidPortName, "Service nginx-ns2/nginx-svc2"},
			{msg.ExternalNameServiceTypeInvalidPortName, "Service nginx-ns3/nginx-svc3"},
		},
	},
	{
		name:       "ExternalNameServiceTypeValidPortName",
		inputFiles: []string{"testdata/correct-port-name-external-name-service-type.yaml"},
		analyzer:   &service.PortNameAnalyzer{},
		expected:   []message{
			// Test no messages are received for correct port name
		},
	},
	{
		name:       "EnvoyFilterUsesRelativeOperation",
		inputFiles: []string{"testdata/relative-envoy-filter-operation.yaml"},
		analyzer:   &envoyfilter.EnvoyPatchAnalyzer{},
		expected: []message{
			{msg.EnvoyFilterUsesRelativeOperation, "EnvoyFilter bookinfo/test-relative-1"},
			{msg.EnvoyFilterUsesRelativeOperation, "EnvoyFilter bookinfo/test-relative-2"},
			{msg.EnvoyFilterUsesRelativeOperation, "EnvoyFilter bookinfo/test-relative-3"},
			{msg.EnvoyFilterUsesRelativeOperationWithProxyVersion, "EnvoyFilter bookinfo/test-relative-3"},
		},
	},
	{
		name:       "EnvoyFilterFilterChainMatch",
		inputFiles: []string{"testdata/envoy-filter-filterchain.yaml"},
		analyzer:   &envoyfilter.EnvoyPatchAnalyzer{},
		expected:   []message{},
	},
	{
		name:       "EnvoyFilterUsesAbsoluteOperation",
		inputFiles: []string{"testdata/absolute-envoy-filter-operation.yaml"},
		analyzer:   &envoyfilter.EnvoyPatchAnalyzer{},
		expected:   []message{
			// Test no messages are received for absolute operation usage
		},
	},
	{
		name:       "EnvoyFilterUsesReplaceOperation",
		inputFiles: []string{"testdata/envoy-filter-replace-operation.yaml"},
		analyzer:   &envoyfilter.EnvoyPatchAnalyzer{},
		expected: []message{
			{msg.EnvoyFilterUsesReplaceOperationIncorrectly, "EnvoyFilter bookinfo/test-replace-1"},
			{msg.EnvoyFilterUsesRelativeOperationWithProxyVersion, "EnvoyFilter bookinfo/test-replace-3"},
			{msg.EnvoyFilterUsesRelativeOperation, "EnvoyFilter bookinfo/test-replace-3"},
		},
	},
	{
		name:       "EnvoyFilterUsesAddOperation",
		inputFiles: []string{"testdata/envoy-filter-add-operation.yaml"},
		analyzer:   &envoyfilter.EnvoyPatchAnalyzer{},
		expected: []message{
			{msg.EnvoyFilterUsesAddOperationIncorrectly, "EnvoyFilter bookinfo/test-auth-2"},
			{msg.EnvoyFilterUsesAddOperationIncorrectly, "EnvoyFilter bookinfo/test-auth-3"},
		},
	},
	{
		name:       "EnvoyFilterUsesRemoveOperation",
		inputFiles: []string{"testdata/envoy-filter-remove-operation.yaml"},
		analyzer:   &envoyfilter.EnvoyPatchAnalyzer{},
		expected: []message{
			{msg.EnvoyFilterUsesRelativeOperation, "EnvoyFilter bookinfo/test-remove-1"},
			{msg.EnvoyFilterUsesRemoveOperationIncorrectly, "EnvoyFilter bookinfo/test-remove-2"},
			{msg.EnvoyFilterUsesRemoveOperationIncorrectly, "EnvoyFilter bookinfo/test-remove-3"},
			{msg.EnvoyFilterUsesRelativeOperation, "EnvoyFilter bookinfo/test-remove-5"},
		},
	},
	{
		name:       "Analyze conflicting gateway with list type",
		inputFiles: []string{"testdata/analyze-list-type.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected: []message{
			{msg.ConflictingGateways, "Gateway alpha"},
			{msg.ConflictingGateways, "Gateway alpha-l"},
			{msg.ConflictingGateways, "Gateway beta-l"},
		},
	},
	{
		name:       "Analyze invalid telemetry",
		inputFiles: []string{"testdata/telemetry-invalid-provider.yaml"},
		analyzer:   &telemetry.ProviderAnalyzer{},
		expected: []message{
			{msg.InvalidTelemetryProvider, "Telemetry istio-system/mesh-default"},
		},
	},
	{
		name:       "Analyze invalid telemetry",
		inputFiles: []string{"testdata/telemetry-disable-provider.yaml"},
		analyzer:   &telemetry.ProviderAnalyzer{},
		expected:   []message{},
	},
	{
		name:       "telemetrySelector",
		inputFiles: []string{"testdata/telemetry-selector.yaml"},
		analyzer:   &telemetry.SelectorAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "Telemetry default/maps-to-nonexistent"},
			{msg.ReferencedResourceNotFound, "Telemetry other/maps-to-different-ns"},
			{msg.ConflictingTelemetryWorkloadSelectors, "Telemetry default/dupe-1"},
			{msg.ConflictingTelemetryWorkloadSelectors, "Telemetry default/dupe-2"},
			{msg.ConflictingTelemetryWorkloadSelectors, "Telemetry default/overlap-1"},
			{msg.ConflictingTelemetryWorkloadSelectors, "Telemetry default/overlap-2"},
		},
	},
	{
		name:       "telemetryDefaultSelector",
		inputFiles: []string{"testdata/telemetry-default-selector.yaml"},
		analyzer:   &telemetry.DefaultSelectorAnalyzer{},
		expected: []message{
			{msg.MultipleTelemetriesWithoutWorkloadSelectors, "Telemetry ns2/has-conflict-2"},
			{msg.MultipleTelemetriesWithoutWorkloadSelectors, "Telemetry ns2/has-conflict-1"},
		},
	},
	{
		name:           "Telemetry Lightstep",
		inputFiles:     []string{"testdata/telemetry-lightstep.yaml"},
		analyzer:       &telemetry.LightstepAnalyzer{},
		meshConfigFile: "testdata/telemetry-lightstep-meshconfig.yaml",
		expected: []message{
			{msg.Deprecated, "Telemetry istio-system/mesh-default"},
		},
	},
	{
		name:       "KubernetesGatewaySelector",
		inputFiles: []string{"testdata/k8sgateway-selector.yaml"},
		analyzer:   &k8sgateway.SelectorAnalyzer{},
		expected: []message{
			{msg.IneffectiveSelector, "RequestAuthentication default/ra-ineffective"},
			{msg.IneffectiveSelector, "AuthorizationPolicy default/ap-ineffective"},
			{msg.IneffectiveSelector, "WasmPlugin default/wasmplugin-ineffective"},
			{msg.IneffectiveSelector, "Telemetry default/telemetry-ineffective"},
		},
	},
	{
		name:       "ServiceEntry Addresses Required Lowercase Protocol",
		inputFiles: []string{"testdata/serviceentry-address-required-lowercase.yaml"},
		analyzer:   &serviceentry.ProtocolAddressesAnalyzer{},
		expected: []message{
			{msg.ServiceEntryAddressesRequired, "ServiceEntry address-missing-lowercase"},
		},
	},
	{
		name:       "ServiceEntry Addresses Required Uppercase Protocol",
		inputFiles: []string{"testdata/serviceentry-address-required-uppercase.yaml"},
		analyzer:   &serviceentry.ProtocolAddressesAnalyzer{},
		expected: []message{
			{msg.ServiceEntryAddressesRequired, "ServiceEntry address-missing-uppercase"},
		},
	},
	{
		name:       "Condition Analyzer",
		inputFiles: []string{"testdata/condition-analyzer.yaml"},
		analyzer:   &conditions.ConditionAnalyzer{},
		expected: []message{
			{msg.NegativeConditionStatus, "Service default/negative-condition-svc"},
			{msg.NegativeConditionStatus, "ServiceEntry default/negative-condition-svcEntry"},
			{msg.NegativeConditionStatus, "AuthorizationPolicy default/negative-condition-authz"},
			{msg.NegativeConditionStatus, "Gateway default/negative-condition-gateway"},
			{msg.NegativeConditionStatus, "HTTPRoute default/negative-condition-httproute"},
			{msg.NegativeConditionStatus, "GRPCRoute default/negative-condition-grpcroute"},
			{msg.NegativeConditionStatus, "AuthorizationPolicy default/negative-condition-authz-partially-invalid"},
		},
	},
	{
		name:       "DestinationRuleWithFakeHost",
		inputFiles: []string{"testdata/destinationrule-with-fake-host.yaml"},
		analyzer:   &destinationrule.PodNotSelectedAnalyzer{},
		expected: []message{
			{msg.UnknownDestinationRuleHost, "DestinationRule default/fake-host"},
		},
	},
	{
		name:       "DestinationRuleSubsetsNotSelectPods",
		inputFiles: []string{"testdata/destinationrule-subsets-not-select-pods.yaml"},
		analyzer:   &destinationrule.PodNotSelectedAnalyzer{},
		expected: []message{
			{msg.DestinationRuleSubsetNotSelectPods, "DestinationRule default/subsets-not-select-pods"},
		},
	},
	{
		name:       "DestinationRuleSubsetsWithTopologyLabels",
		inputFiles: []string{"testdata/destinationrule-subsets-with-topology-labels.yaml"},
		analyzer:   &destinationrule.PodNotSelectedAnalyzer{},
		expected:   []message{
			// Should not report false positives for topology labels
			// All subsets should match because the analyzer augments pod labels with node topology labels:
			// - "region-us-west": matches topology.kubernetes.io/region from node
			// - "zone-us-west-1a": matches topology.kubernetes.io/zone from node
			// - "app-v1": matches version label from pod
			// - "mixed-labels": matches both version from pod AND topology.kubernetes.io/region from node
		},
	},
	{
		name:       "DestinationRuleEmptyTopologyLabels",
		inputFiles: []string{"testdata/destinationrule-empty-topology-labels.yaml"},
		analyzer:   &destinationrule.PodNotSelectedAnalyzer{},
		expected: []message{
			// Istio doesn't match on empty node locality labels.
			{msg.DestinationRuleSubsetNotSelectPods, "DestinationRule default/empty-topology-labels"},
		},
	},
	{
		name:           "ServiceEntry Addresses Allocated",
		inputFiles:     []string{"testdata/serviceentry-address-allocated.yaml"},
		meshConfigFile: "testdata/serviceentry-address-allocated-mesh-cfg.yaml",
		analyzer:       &serviceentry.ProtocolAddressesAnalyzer{},
		expected:       []message{},
	},
}

// regex patterns for analyzer names that should be explicitly ignored for testing
var ignoreAnalyzers = []string{
	// ValidationAnalyzer doesn't have any of its own logic, it just wraps the schema validation.
	// We assume that detailed testing for schema validation is being done elsewhere.
	// Testing the ValidationAnalyzer as a wrapper is done in a separate unit test.)
	`schema\.ValidationAnalyzer\.*`,
}

// TestAnalyzers allows for table-based testing of Analyzers.
func TestAnalyzers(t *testing.T) {
	requestedInputsByAnalyzer := make(map[string]map[config.GroupVersionKind]struct{})

	// For each test case, verify we get the expected messages as output
	for _, tc := range testGrid {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// Set up a hook to record which collections are accessed by each analyzer
			analyzerName := tc.analyzer.Metadata().Name
			cr := func(col config.GroupVersionKind) {
				if _, ok := requestedInputsByAnalyzer[analyzerName]; !ok {
					requestedInputsByAnalyzer[analyzerName] = make(map[config.GroupVersionKind]struct{})
				}
				requestedInputsByAnalyzer[analyzerName][col] = struct{}{}
			}

			// Set up Analyzer for this test case
			sa, err := setupAnalyzerForCase(tc, cr)
			if err != nil {
				t.Fatalf("Error setting up analysis for testcase %s: %v", tc.name, err)
			}

			// Run the analysis
			result, err := runAnalyzer(sa)
			if err != nil {
				t.Fatalf("Error running analysis on testcase %s: %v", tc.name, err)
			}

			g.Expect(extractFields(result.Messages)).To(ConsistOf(tc.expected), "%v", prettyPrintMessages(result.Messages))
		})
	}

	// Verify that the collections actually accessed during testing actually match
	// the collections declared as inputs for each of the analyzers
	t.Run("CheckMetadataInputs", func(t *testing.T) {
		g := NewWithT(t)
	outer:
		for _, a := range All() {
			var isMultiClusterAnalyzer bool
			for _, mc := range AllMultiCluster() {
				if a.Metadata().Name == mc.Metadata().Name {
					isMultiClusterAnalyzer = true
					break
				}
			}
			if isMultiClusterAnalyzer {
				continue
			}
			analyzerName := a.Metadata().Name

			// Skip this check for explicitly ignored analyzers
			for _, regex := range ignoreAnalyzers {
				match, err := regexp.MatchString(regex, analyzerName)
				if err != nil {
					t.Fatalf("Error compiling ignoreAnalyzers regex %q: %v", regex, err)
				}
				if match {
					continue outer
				}
			}

			requestedInputs := make([]config.GroupVersionKind, 0)
			for col := range requestedInputsByAnalyzer[analyzerName] {
				requestedInputs = append(requestedInputs, col)
			}

			g.Expect(a.Metadata().Inputs).To(ConsistOf(requestedInputs), fmt.Sprintf(
				"Metadata inputs for analyzer %q don't match actual collections accessed during testing. "+
					"Either the metadata is wrong or the test cases for the analyzer are insufficient.", analyzerName))
		}
	})
}

// Verify that all of the analyzers tested here are also registered in All()
func TestAnalyzersInAll(t *testing.T) {
	g := NewWithT(t)

	var allNames []string
	for _, a := range All() {
		allNames = append(allNames, a.Metadata().Name)
	}

	for _, tc := range testGrid {
		if !tc.skipAll {
			g.Expect(allNames).To(ContainElement(tc.analyzer.Metadata().Name))
		}
	}
}

func TestAnalyzersHaveUniqueNames(t *testing.T) {
	g := NewWithT(t)

	existingNames := sets.New[string]()
	for _, a := range All() {
		n := a.Metadata().Name
		// TODO (Nino-K): remove this condition once metadata is clean up
		if existingNames.Contains(n) && n == "schema.ValidationAnalyzer.ServiceEntry" {
			continue
		}
		g.Expect(existingNames.Contains(n)).To(BeFalse(), fmt.Sprintf("Analyzer name %q is used more than once. "+
			"Analyzers should be registered in All() exactly once and have a unique name.", n))

		existingNames.Insert(n)
	}
}

func TestAnalyzersHaveDescription(t *testing.T) {
	g := NewWithT(t)

	for _, a := range All() {
		g.Expect(a.Metadata().Description).ToNot(Equal(""))
	}
}

func setupAnalyzerForCase(tc testCase, cr local.CollectionReporterFn) (*local.IstiodAnalyzer, error) {
	sa := local.NewSourceAnalyzer(analysis.Combine("testCase", tc.analyzer), "", "istio-system", cr)

	// If a mesh config file is specified, use it instead of the defaults
	if tc.meshConfigFile != "" {
		err := sa.AddFileKubeMeshConfig(tc.meshConfigFile)
		if err != nil {
			return nil, fmt.Errorf("error applying mesh config file %s: %v", tc.meshConfigFile, err)
		}
	}

	if tc.meshNetworksFile != "" {
		err := sa.AddFileKubeMeshNetworks(tc.meshNetworksFile)
		if err != nil {
			return nil, fmt.Errorf("error apply mesh networks file %s: %v", tc.meshNetworksFile, err)
		}
	}

	// Gather test files
	var files []local.ReaderSource
	for _, f := range tc.inputFiles {
		of, err := os.Open(f)
		if err != nil {
			return nil, fmt.Errorf("error opening test file: %q", f)
		}
		files = append(files, local.ReaderSource{Name: f, Reader: of})
	}

	// Include resources from test files
	err := sa.AddTestReaderKubeSource(files)
	if err != nil {
		return nil, fmt.Errorf("error setting up file kube source on testcase %s: %v", tc.name, err)
	}

	// Include default resources
	err = sa.AddDefaultResources()
	if err != nil {
		return nil, fmt.Errorf("error adding default resources: %v", err)
	}

	return sa, nil
}

func runAnalyzer(sa *local.IstiodAnalyzer) (local.AnalysisResult, error) {
	cancel := make(chan struct{})
	result, err := sa.Analyze(cancel)
	if err != nil {
		return local.AnalysisResult{}, err
	}
	return result, err
}

// Pull just the fields we want to check out of diag.Message
func extractFields(msgs diag.Messages) []message {
	result := make([]message, 0)
	for _, m := range msgs {
		expMsg := message{
			messageType: m.Type,
		}
		if m.Resource != nil {
			expMsg.origin = m.Resource.Origin.FriendlyName()
		}

		result = append(result, expMsg)
	}
	return result
}

func prettyPrintMessages(msgs diag.Messages) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Analyzer messages: %d\n", len(msgs))
	for _, m := range msgs {
		fmt.Fprintf(&sb, "\t%s\n", m.String())
	}
	return sb.String()
}
