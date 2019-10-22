// Copyright 2019 Istio Authors
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
	"regexp"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/annotations"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/auth"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/deprecation"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/gateway"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/injection"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/virtualservice"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/meta/metadata"
	"istio.io/istio/galley/pkg/config/meta/schema/collection"
)

type message struct {
	messageType *diag.MessageType
	origin      string
}

type testCase struct {
	name       string
	inputFiles []string
	analyzer   analysis.Analyzer
	expected   []message
}

// Some notes on setting up tests for Analyzers:
// * The resources in the input files don't necessarily need to be completely defined, just defined enough for the analyzer being tested.
// * Please keep this list sorted alphabetically by the pkg.name of the analyzer for convenience
// * Expected messages are in the format {msg.ValidationMessageType, "<ResourceKind>/<Namespace>/<ResourceName>"}.
//     * Note that if Namespace is omitted in the input YAML, it will be skipped here.
var testGrid = []testCase{
	{
		name:       "serviceRoleBindings",
		inputFiles: []string{"testdata/servicerolebindings.yaml"},
		analyzer:   &auth.ServiceRoleBindingAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "ServiceRoleBinding test-bogus-binding"},
		},
	},
	{
		name:       "deprecation",
		inputFiles: []string{"testdata/deprecation.yaml"},
		analyzer:   &deprecation.FieldAnalyzer{},
		expected: []message{
			{msg.Deprecated, "VirtualService route-egressgateway"},
			{msg.Deprecated, "VirtualService tornado"},
			{msg.Deprecated, "EnvoyFilter istio-multicluster-egressgateway.istio-system"},
			{msg.Deprecated, "EnvoyFilter istio-multicluster-egressgateway.istio-system"}, // Duplicate, because resource has two problems
			{msg.Deprecated, "ServiceRoleBinding bind-mongodb-viewer.default"},
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
		name:       "istioInjection",
		inputFiles: []string{"testdata/injection.yaml"},
		analyzer:   &injection.Analyzer{},
		expected: []message{
			{msg.NamespaceNotInjected, "Namespace bar"},
			{msg.PodMissingProxy, "Pod noninjectedpod.default"},
		},
	},
	{
		name:       "istioInjectionVersionMismatch",
		inputFiles: []string{"testdata/injection-with-mismatched-sidecar.yaml"},
		analyzer:   &injection.VersionAnalyzer{},
		expected: []message{
			{msg.IstioProxyVersionMismatch, "Pod details-v1-pod-old.enabled-namespace"},
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
		},
	},
	{
		name:       "virtualServiceDestinationRules",
		inputFiles: []string{"testdata/virtualservice_destinationrules.yaml"},
		analyzer:   &virtualservice.DestinationRuleAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService reviews-bogussubset.default"},
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
		name: "misannoted",
		inputFiles: []string{
			"testdata/misannotated.yaml",
		},
		analyzer: &annotations.K8sAnalyzer{},
		expected: []message{
			{msg.UnknownAnnotation, "Service httpbin"},
			{msg.MisplacedAnnotation, "Service details"},
			{msg.MisplacedAnnotation, "Pod grafana-test"},
			{msg.MisplacedAnnotation, "Deployment fortio-deploy"},
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
	for _, testCase := range testGrid {
		testCase := testCase // Capture range variable so subtests work correctly
		t.Run(testCase.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			// Set up a hook to record which collections are accessed by each analyzer
			analyzerName := testCase.analyzer.Metadata().Name
			cr := func(col collection.Name) {
				if _, ok := requestedInputsByAnalyzer[analyzerName]; !ok {
					requestedInputsByAnalyzer[analyzerName] = make(map[collection.Name]struct{})
				}
				requestedInputsByAnalyzer[analyzerName][col] = struct{}{}
			}

			sa := local.NewSourceAnalyzer(metadata.MustGet(), analysis.Combine("testCombined", testCase.analyzer), "", cr, true)

			err := sa.AddFileKubeSource(testCase.inputFiles)
			if err != nil {
				t.Fatalf("Error setting up file kube source on testcase %s: %v", testCase.name, err)
			}
			cancel := make(chan struct{})

			msgs, err := sa.Analyze(cancel)
			if err != nil {
				t.Fatalf("Error running analysis on testcase %s: %v", testCase.name, err)
			}

			actualMsgs := extractFields(msgs)
			g.Expect(actualMsgs).To(ConsistOf(testCase.expected))
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

func TestAnalyzersHaveUniqueNames(t *testing.T) {
	g := NewGomegaWithT(t)

	existingNames := make(map[string]struct{})
	for _, a := range All() {
		n := a.Metadata().Name
		_, ok := existingNames[n]
		g.Expect(ok).To(BeFalse(), fmt.Sprintf("Analyzer name %q is used more than once. "+
			"Analyzers should be registered in All() exactly once and have a unique name.", n))

		existingNames[n] = struct{}{}
	}
}

// Pull just the fields we want to check out of diag.Message
func extractFields(msgs []diag.Message) []message {
	result := make([]message, 0)
	for _, m := range msgs {
		expMsg := message{
			messageType: m.Type,
		}
		if m.Origin != nil {
			expMsg.origin = m.Origin.FriendlyName()
		}
		result = append(result, expMsg)
	}
	return result
}
