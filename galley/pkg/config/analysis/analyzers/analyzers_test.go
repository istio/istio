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
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/auth"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/virtualservice"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
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
var testGrid = []testCase{
	{
		name: "serviceRoleBindings",
		inputFiles: []string{
			"testdata/servicerolebindings.yaml",
		},
		analyzer: &auth.ServiceRoleBindingAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "ServiceRoleBinding/test-bogus-binding"},
		},
	},
	{
		name: "virtualServiceGateways",
		inputFiles: []string{
			"testdata/virtualservice_gateways.yaml",
		},
		analyzer: &virtualservice.GatewayAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService/httpbin-bogus"},
		},
	},
	{
		name: "virtualServiceDestinations",
		inputFiles: []string{
			"testdata/virtualservice_destinations.yaml",
		},
		analyzer: &virtualservice.DestinationAnalyzer{},
		expected: []message{
			{msg.ReferencedResourceNotFound, "VirtualService/default/reviews-bogushost"},
			{msg.ReferencedResourceNotFound, "VirtualService/default/reviews-bogussubset"},
		},
	},
}

// TestAnalyzers allows for table-based testing of Analyzers.
func TestAnalyzers(t *testing.T) {
	for _, testCase := range testGrid {
		testCase := testCase // Capture range variable so subtests work correctly
		t.Run(testCase.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			sa := local.NewSourceAnalyzer(metadata.MustGet(), testCase.analyzer)
			sa.AddFileKubeSource(testCase.inputFiles, "")
			cancel := make(chan struct{})
			msgs, err := sa.Analyze(cancel)
			if err != nil {
				t.Fatalf("Error running analysis on testcase %s: %v", testCase.name, err)
			}
			actualMsgs := extractFields(msgs)
			g.Expect(actualMsgs).To(ConsistOf(testCase.expected))
		})
	}
}

// Pull just the fields we want to check out of diag.Message
func extractFields(msgs []diag.Message) []message {
	result := make([]message, 0)
	for _, m := range msgs {
		result = append(result, message{
			messageType: m.Type,
			origin:      m.Origin.FriendlyName(),
		})
	}
	return result
}
