package analyzers

import (
	"fmt"
	"testing"

	. "github.com/onsi/gomega"

	"istio.io/istio/galley/pkg/config/analysis"
	"istio.io/istio/galley/pkg/config/analysis/analyzers/auth"
	"istio.io/istio/galley/pkg/config/analysis/diag"
	"istio.io/istio/galley/pkg/config/analysis/local"
	"istio.io/istio/galley/pkg/config/analysis/msg"
	"istio.io/istio/galley/pkg/config/processor/metadata"
)

type message struct {
	messageType diag.MessageType
	origin      string
}

type testCase struct {
	name       string
	inputFiles []string
	analyzer   analysis.Analyzer
	expected   []message
}

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
}

func TestAnalyzers(t *testing.T) {
	for _, testCase := range testGrid {
		testCase := testCase //Capture range variable so subtests work correctly
		t.Run(testCase.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			sa := local.NewSourceAnalyzer(metadata.MustGet(), testCase.analyzer)
			sa.AddFileKubeSource(testCase.inputFiles)
			//TODO: Do we need some way to cancel more intelligently?
			cancel := make(chan struct{})
			msgs, err := sa.Analyze(cancel)
			if err != nil {
				t.Fatalf("Error running analysis on testcase %s: %v", testCase.name, err)
			}
			fmt.Println("DEBUG2a", msgs)

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
			messageType: m.MessageType,
			origin:      m.Origin.FriendlyName(),
		})
	}
	return result
}
