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

	"istio.io/istio/galley/pkg/config/analysis/analyzers/destinationrule"

	. "github.com/onsi/gomega"

	"istio.io/pkg/log"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/annotations"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/authz"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/deployment"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/deprecation"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/gateway"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/injection"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/multicluster"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/service"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/sidecar"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/virtualservice"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processing/snapshotter"
	"istio.io/istio/galley/pkg/config/scope"
	"istio.io/istio/pkg/config/schema"
	"istio.io/istio/pkg/config/schema/collection"
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
}

// Some notes on setting up tests for Analyzers:
// * The resources in the input files don't necessarily need to be completely defined, just defined enough for the analyzer being tested.
// * Please keep this list sorted alphabetically by the pkg.name of the analyzer for convenience
// * Expected messages are in the format {msg.ValidationMessageType, "<ResourceKind>/<Namespace>/<ResourceName>"}.
//     * Note that if Namespace is omitted in the input YAML, it will be skipped here.
var testGrid = []testCase{
	{
		name: "misannoted",
		inputFiles: []string{
			"testdata/misannotated.yaml",
		},
		analyzer: &annotations.K8sAnalyzer{},
		expected: []message{
			{msg.UnknownAnnotation, "Service httpbin"},
			{msg.MisplacedAnnotation, "Service details"},
			{msg.InvalidAnnotation, "Pod invalid-annotations"},
			{msg.MisplacedAnnotation, "Pod grafana-test"},
			{msg.MisplacedAnnotation, "Deployment fortio-deploy"},
			{msg.MisplacedAnnotation, "Namespace staging"},
		},
	},
	{
		name:       "deprecation",
		inputFiles: []string{"testdata/deprecation.yaml"},
		analyzer:   &deprecation.FieldAnalyzer{},
		expected: []message{
			{msg.Deprecated, "VirtualService productpage.foo"},
			{msg.Deprecated, "Sidecar no-selector.default"},
			{msg.Deprecated, "QuotaSpec request-count.istio-system"},
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
		name:       "istioInjection",
		inputFiles: []string{"testdata/injection.yaml"},
		analyzer:   &injection.Analyzer{},
		expected: []message{
			{msg.NamespaceNotInjected, "Namespace bar"},
			{msg.PodMissingProxy, "Pod noninjectedpod.default"},
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
			{msg.IstioProxyImageMismatch, "Pod details-v1-pod-old.enabled-namespace"},
		},
	},
	{
		name:       "portNameNotFollowConvention",
		inputFiles: []string{"testdata/service-no-port-name.yaml"},
		analyzer:   &service.PortNameAnalyzer{},
		expected: []message{
			{msg.PortNameIsNotUnderNamingConvention, "Service my-service1.my-namespace1"},
			{msg.PortNameIsNotUnderNamingConvention, "Service my-service1.my-namespace1"},
			{msg.PortNameIsNotUnderNamingConvention, "Service my-service2.my-namespace2"},
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
			{msg.MultipleSidecarsWithoutWorkloadSelectors, "Sidecar has-conflict-2.ns2"},
			{msg.MultipleSidecarsWithoutWorkloadSelectors, "Sidecar has-conflict-1.ns2"},
		},
	},
	{
		name:       "sidecarSelector",
		inputFiles: []string{"testdata/sidecar-selector.yaml"},
		analyzer:   &sidecar.SelectorAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "Sidecar maps-to-nonexistent.default"},
			{msg.ReferencedResourceNotFound, "Sidecar maps-to-different-ns.other"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar dupe-1.default"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar dupe-2.default"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar overlap-1.default"},
			{msg.ConflictingSidecarWorkloadSelectors, "Sidecar overlap-2.default"},
		},
	},
	{
		name:       "virtualServiceConflictingMeshGatewayHosts",
		inputFiles: []string{"testdata/virtualservice_conflictingmeshgatewayhosts.yaml"},
		analyzer:   &virtualservice.ConflictingMeshGatewayHostsAnalyzer{},
		expected: []message{
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService ratings.team3"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService ratings.team4"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService ratings.foo"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService ratings.bar"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService productpage.foo"},
			{msg.ConflictingMeshGatewayVirtualServiceHosts, "VirtualService bogus-productpage.foo"},
		},
	},
	{
		name:       "virtualServiceDestinationHosts",
		inputFiles: []string{"testdata/virtualservice_destinationhosts.yaml"},
		analyzer:   &virtualservice.DestinationHostAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService reviews-bogushost.default"},
			{msg.ReferencedResourceNotFound, "VirtualService reviews-bookinfo-other.default"},
			{msg.ReferencedResourceNotFound, "VirtualService reviews-mirror-bogushost.default"},
			{msg.ReferencedResourceNotFound, "VirtualService reviews-bogusport.default"},
			{msg.VirtualServiceDestinationPortSelectorRequired, "VirtualService reviews-2port-missing.default"},
			{msg.ReferencedResourceNotFound, "VirtualService cross-namespace-details.istio-system"},
		},
	},
	{
		name:       "virtualServiceDestinationRules",
		inputFiles: []string{"testdata/virtualservice_destinationrules.yaml"},
		analyzer:   &virtualservice.DestinationRuleAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService reviews-bogussubset.default"},
			{msg.ReferencedResourceNotFound, "VirtualService reviews-mirror-bogussubset.default"},
		},
	},
	{
		name:       "virtualServiceGateways",
		inputFiles: []string{"testdata/virtualservice_gateways.yaml"},
		analyzer:   &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService httpbin-bogus"},
		},
	},
	{
		name:       "serviceMultipleDeployments",
		inputFiles: []string{"testdata/deployment-multi-service.yaml"},
		analyzer:   &deployment.ServiceAssociationAnalyzer{},
		expected: []message{
			{msg.DeploymentAssociatedToMultipleServices, "Deployment multiple-svc-multiple-prot.bookinfo"},
			{msg.DeploymentAssociatedToMultipleServices, "Deployment multiple-without-port.bookinfo"},
			{msg.DeploymentRequiresServiceAssociated, "Deployment no-services.bookinfo"},
			{msg.DeploymentRequiresServiceAssociated, "Deployment ann-enabled-ns-disabled.injection-disabled-ns"},
		},
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
			{msg.UnknownMeshNetworksServiceRegistry, "MeshNetworks meshnetworks.istio-system"},
			{msg.UnknownMeshNetworksServiceRegistry, "MeshNetworks meshnetworks.istio-system"},
		},
	},
	{
		name: "authorizationpolicies",
		inputFiles: []string{
			"testdata/authorizationpolicies.yaml",
		},
		analyzer: &authz.AuthorizationPoliciesAnalyzer{},
		expected: []message{
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy meshwide-httpbin-v1.istio-system"},
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy httpbin-empty-namespace-wide.httpbin-empty"},
			{msg.NoMatchingWorkloadsFound, "AuthorizationPolicy httpbin-nopods.httpbin"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin-bogus-ns.httpbin"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin-bogus-ns.httpbin"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin-bogus-not-ns.httpbin"},
			{msg.ReferencedResourceNotFound, "AuthorizationPolicy httpbin-bogus-not-ns.httpbin"},
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
			g := NewGomegaWithT(t)

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
		g := NewGomegaWithT(t)
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
	g := NewGomegaWithT(t)

	var allNames []string
	for _, a := range All() {
		allNames = append(allNames, a.Metadata().Name)
	}

	for _, tc := range testGrid {
		g.Expect(allNames).To(ContainElement(tc.analyzer.Metadata().Name))
	}
}

func TestAnalyzersHaveUniqueNames(t *testing.T) {
	g := NewGomegaWithT(t)

	existingNames := make(map[string]struct{})
	for _, a := range All() {
		n := a.Metadata().Name
		_, ok := existingNames[n]
		// TODO (Nino-K): remove this condition once metadata is clean up
		if ok == true && n == "schema.ValidationAnalyzer.ServiceEntry" {
			continue
		}
		g.Expect(ok).To(BeFalse(), fmt.Sprintf("Analyzer name %q is used more than once. "+
			"Analyzers should be registered in All() exactly once and have a unique name.", n))

		existingNames[n] = struct{}{}
	}
}

func TestAnalyzersHaveDescription(t *testing.T) {
	g := NewGomegaWithT(t)

	for _, a := range All() {
		g.Expect(a.Metadata().Description).ToNot(Equal(""))
	}
}

func setupAnalyzerForCase(tc testCase, cr snapshotter.CollectionReporterFn) (*local.SourceAnalyzer, error) {
	sa := local.NewSourceAnalyzer(schema.MustGet(), analysis.Combine("testCase", tc.analyzer), "", "istio-system", cr, true, 10*time.Second)

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

func runAnalyzer(sa *local.SourceAnalyzer) (local.AnalysisResult, error) {
	// Default processing log level is too chatty for these tests
	prevLogLevel := scope.Processing.GetOutputLevel()
	scope.Processing.SetOutputLevel(log.ErrorLevel)
	defer scope.Processing.SetOutputLevel(prevLogLevel)

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
