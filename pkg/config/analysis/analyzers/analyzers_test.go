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
	"time"

	. "github.com/onsi/gomega"

	"istio.io/istio/pkg/config/analysis"
	"istio.io/istio/pkg/config/analysis/analyzers/annotations"
	"istio.io/istio/pkg/config/analysis/analyzers/authz"
	"istio.io/istio/pkg/config/analysis/analyzers/deployment"
	"istio.io/istio/pkg/config/analysis/analyzers/deprecation"
	"istio.io/istio/pkg/config/analysis/analyzers/destinationrule"
	"istio.io/istio/pkg/config/analysis/analyzers/envoyfilter"
	"istio.io/istio/pkg/config/analysis/analyzers/gateway"
	"istio.io/istio/pkg/config/analysis/analyzers/injection"
	"istio.io/istio/pkg/config/analysis/analyzers/maturity"
	"istio.io/istio/pkg/config/analysis/analyzers/multicluster"
	schemaValidation "istio.io/istio/pkg/config/analysis/analyzers/schema"
	"istio.io/istio/pkg/config/analysis/analyzers/service"
	"istio.io/istio/pkg/config/analysis/analyzers/serviceentry"
	"istio.io/istio/pkg/config/analysis/analyzers/sidecar"
	"istio.io/istio/pkg/config/analysis/analyzers/virtualservice"
	"istio.io/istio/pkg/config/analysis/analyzers/webhook"
	"istio.io/istio/pkg/config/analysis/diag"
	"istio.io/istio/pkg/config/analysis/local"
	"istio.io/istio/pkg/config/analysis/msg"
	"istio.io/istio/pkg/config/schema/collection"
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
			{msg.GatewayPortNotOnWorkload, "Gateway httpbin-gateway"},
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
			{msg.GatewayPortNotOnWorkload, "Gateway httpbin-gateway"},
		},
	},
	{
		name:       "gatewayServiceMatchPod",
		inputFiles: []string{"testdata/gateway-custom-ingressgateway-svcselector.yaml"},
		analyzer:   &gateway.IngressGatewayPortAnalyzer{},
		expected: []message{
			{msg.GatewayPortNotOnWorkload, "Gateway httpbin8002-gateway"},
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
		name:       "conflicting gateways detect",
		inputFiles: []string{"testdata/conflicting-gateways.yaml"},
		analyzer:   &gateway.ConflictingGatewayAnalyzer{},
		expected: []message{
			{msg.ConflictingGateways, "Gateway alpha"},
			{msg.ConflictingGateways, "Gateway beta"},
		},
	},
	{
		name:       "istioInjection",
		inputFiles: []string{"testdata/injection.yaml"},
		analyzer:   &injection.Analyzer{},
		expected: []message{
			{msg.NamespaceNotInjected, "Namespace bar"},
			{msg.PodMissingProxy, "Pod default/noninjectedpod"},
			{msg.NamespaceMultipleInjectionLabels, "Namespace busted"},
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
			{msg.IstioProxyImageMismatch, "Pod enabled-namespace/details-v1-pod-old"},
		},
	},
	{
		name: "istioInjectionProxyImageMismatchAbsolute",
		inputFiles: []string{
			"testdata/injection-with-mismatched-sidecar.yaml",
			"testdata/sidecar-injector-configmap-absolute-override.yaml",
		},
		analyzer: &injection.ImageAnalyzer{},
		expected: []message{
			{msg.IstioProxyImageMismatch, "Pod enabled-namespace/details-v1-pod-old"},
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
			{msg.IstioProxyImageMismatch, "Pod enabled-namespace/details-v1-pod-old"},
			{msg.IstioProxyImageMismatch, "Pod revision-namespace/revision-details-v1-pod-old"},
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
		analyzer:   &sidecar.DefaultSelectorAnalyzer{},
		expected: []message{
			{msg.MultipleSidecarsWithoutWorkloadSelectors, "Sidecar ns2/has-conflict-2"},
			{msg.MultipleSidecarsWithoutWorkloadSelectors, "Sidecar ns2/has-conflict-1"},
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
		name:       "serviceMultipleDeployments",
		inputFiles: []string{"testdata/deployment-multi-service.yaml"},
		analyzer:   &deployment.ServiceAssociationAnalyzer{},
		expected: []message{
			{msg.DeploymentAssociatedToMultipleServices, "Deployment bookinfo/multiple-svc-multiple-prot"},
			{msg.DeploymentAssociatedToMultipleServices, "Deployment bookinfo/multiple-without-port"},
			{msg.DeploymentRequiresServiceAssociated, "Deployment bookinfo/no-services"},
			{msg.DeploymentRequiresServiceAssociated, "Deployment injection-disabled-ns/ann-enabled-ns-disabled"},
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
		name: "regexes",
		inputFiles: []string{
			"testdata/virtualservice_regexes.yaml",
		},
		analyzer: &virtualservice.RegexAnalyzer{},
		expected: []message{
			{msg.InvalidRegexp, "VirtualService bad-match"},
			{msg.InvalidRegexp, "VirtualService ecma-not-v2"},
			{msg.InvalidRegexp, "VirtualService lots-of-regexes"},
			{msg.InvalidRegexp, "VirtualService lots-of-regexes"},
			{msg.InvalidRegexp, "VirtualService lots-of-regexes"},
			{msg.InvalidRegexp, "VirtualService lots-of-regexes"},
			{msg.InvalidRegexp, "VirtualService lots-of-regexes"},
			{msg.InvalidRegexp, "VirtualService lots-of-regexes"},
		},
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
		analyzer: schemaValidation.CollectionValidationAnalyzer(collections.IstioNetworkingV1Alpha3Virtualservices),
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
	requestedInputsByAnalyzer := make(map[string]map[collection.Name]struct{})

	// For each test case, verify we get the expected messages as output
	for _, tc := range testGrid {
		tc := tc // Capture range variable so subtests work correctly
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)

			// Set up a hook to record which collections are accessed by each analyzer
			analyzerName := tc.analyzer.Metadata().Name
			cr := func(col collection.Name) {
				if _, ok := requestedInputsByAnalyzer[analyzerName]; !ok {
					requestedInputsByAnalyzer[analyzerName] = make(map[collection.Name]struct{})
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

			requestedInputs := make([]collection.Name, 0)
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

	existingNames := sets.New()
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
	sa := local.NewSourceAnalyzer(analysis.Combine("testCase", tc.analyzer), "", "istio-system", cr, true, 10*time.Second)

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

	// Include default resources
	err := sa.AddDefaultResources()
	if err != nil {
		return nil, fmt.Errorf("error adding default resources: %v", err)
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
	err = sa.AddReaderKubeSource(files)
	if err != nil {
		return nil, fmt.Errorf("error setting up file kube source on testcase %s: %v", tc.name, err)
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
