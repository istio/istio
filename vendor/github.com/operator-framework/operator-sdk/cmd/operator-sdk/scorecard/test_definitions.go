// Copyright 2019 The Operator-SDK Authors
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

package scorecard

import (
	"context"

	olmapiv1alpha1 "github.com/operator-framework/operator-lifecycle-manager/pkg/api/apis/operators/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Type Definitions

// Test provides methods for running scorecard tests
type Test interface {
	GetName() string
	GetDescription() string
	IsCumulative() bool
	Run(context.Context) *TestResult
}

// TestResult contains a test's points, suggestions, and errors
type TestResult struct {
	Test          Test
	EarnedPoints  int
	MaximumPoints int
	Suggestions   []string
	Errors        []error
}

// TestInfo contains information about the scorecard test
type TestInfo struct {
	Name        string
	Description string
	// If a test is set to cumulative, the scores of multiple runs of the same test on separate CRs are added together for the total score.
	// If cumulative is false, if any test failed, the total score is 0/1. Otherwise 1/1.
	Cumulative bool
}

// GetName return the test name
func (i TestInfo) GetName() string { return i.Name }

// GetDescription returns the test description
func (i TestInfo) GetDescription() string { return i.Description }

// IsCumulative returns true if the test's scores are intended to be cumulative
func (i TestInfo) IsCumulative() bool { return i.Cumulative }

// BasicTestConfig contains all variables required by the BasicTest TestSuite
type BasicTestConfig struct {
	Client   client.Client
	CR       *unstructured.Unstructured
	ProxyPod *v1.Pod
}

// OLMTestConfig contains all variables required by the OLMTest TestSuite
type OLMTestConfig struct {
	Client   client.Client
	CR       *unstructured.Unstructured
	CSV      *olmapiv1alpha1.ClusterServiceVersion
	CRDsDir  string
	ProxyPod *v1.Pod
}

// TestSuite contains a list of tests and results, along with the relative weights of each test
type TestSuite struct {
	TestInfo
	Tests       []Test
	TestResults []*TestResult
	Weights     map[string]float64
}

// Test definitions

// CheckSpecTest is a scorecard test that verifies that the CR has a spec block
type CheckSpecTest struct {
	TestInfo
	BasicTestConfig
}

// NewCheckSpecTest returns a new CheckSpecTest object
func NewCheckSpecTest(conf BasicTestConfig) *CheckSpecTest {
	return &CheckSpecTest{
		BasicTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Spec Block Exists",
			Description: "Custom Resource has a Spec Block",
			Cumulative:  false,
		},
	}
}

// CheckStatusTest is a scorecard test that verifies that the CR has a status block
type CheckStatusTest struct {
	TestInfo
	BasicTestConfig
}

// NewCheckStatusTest returns a new CheckStatusTest object
func NewCheckStatusTest(conf BasicTestConfig) *CheckStatusTest {
	return &CheckStatusTest{
		BasicTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Status Block Exists",
			Description: "Custom Resource has a Status Block",
			Cumulative:  false,
		},
	}
}

// WritingIntoCRsHasEffectTest is a scorecard test that verifies that the operator is making PUT and/or POST requests to the API server
type WritingIntoCRsHasEffectTest struct {
	TestInfo
	BasicTestConfig
}

// NewWritingIntoCRsHasEffectTest returns a new WritingIntoCRsHasEffectTest object
func NewWritingIntoCRsHasEffectTest(conf BasicTestConfig) *WritingIntoCRsHasEffectTest {
	return &WritingIntoCRsHasEffectTest{
		BasicTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Writing into CRs has an effect",
			Description: "A CR sends PUT/POST requests to the API server to modify resources in response to spec block changes",
			Cumulative:  false,
		},
	}
}

// CRDsHaveValidationTest is a scorecard test that verifies that all CRDs have a validation section
type CRDsHaveValidationTest struct {
	TestInfo
	OLMTestConfig
}

// NewCRDsHaveValidationTest returns a new CRDsHaveValidationTest object
func NewCRDsHaveValidationTest(conf OLMTestConfig) *CRDsHaveValidationTest {
	return &CRDsHaveValidationTest{
		OLMTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Provided APIs have validation",
			Description: "All CRDs have an OpenAPI validation subsection",
			Cumulative:  true,
		},
	}
}

// CRDsHaveResourcesTest is a scorecard test that verifies that the CSV lists used resources in its owned CRDs secyion
type CRDsHaveResourcesTest struct {
	TestInfo
	OLMTestConfig
}

// NewCRDsHaveResourcesTest returns a new CRDsHaveResourcesTest object
func NewCRDsHaveResourcesTest(conf OLMTestConfig) *CRDsHaveResourcesTest {
	return &CRDsHaveResourcesTest{
		OLMTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Owned CRDs have resources listed",
			Description: "All Owned CRDs contain a resources subsection",
			Cumulative:  true,
		},
	}
}

// AnnotationsContainExamplesTest is a scorecard test that verifies that the CSV contains examples via the alm-examples annotation
type AnnotationsContainExamplesTest struct {
	TestInfo
	OLMTestConfig
}

// NewAnnotationsContainExamplesTest returns a new AnnotationsContainExamplesTest object
func NewAnnotationsContainExamplesTest(conf OLMTestConfig) *AnnotationsContainExamplesTest {
	return &AnnotationsContainExamplesTest{
		OLMTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "CRs have at least 1 example",
			Description: "The CSV's metadata contains an alm-examples section",
			Cumulative:  true,
		},
	}
}

// SpecDescriptorsTest is a scorecard test that verifies that all spec fields have descriptors
type SpecDescriptorsTest struct {
	TestInfo
	OLMTestConfig
}

// NewSpecDescriptorsTest returns a new SpecDescriptorsTest object
func NewSpecDescriptorsTest(conf OLMTestConfig) *SpecDescriptorsTest {
	return &SpecDescriptorsTest{
		OLMTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Spec fields with descriptors",
			Description: "All spec fields have matching descriptors in the CSV",
			Cumulative:  true,
		},
	}
}

// StatusDescriptorsTest is a scorecard test that verifies that all status fields have descriptors
type StatusDescriptorsTest struct {
	TestInfo
	OLMTestConfig
}

// NewStatusDescriptorsTest returns a new StatusDescriptorsTest object
func NewStatusDescriptorsTest(conf OLMTestConfig) *StatusDescriptorsTest {
	return &StatusDescriptorsTest{
		OLMTestConfig: conf,
		TestInfo: TestInfo{
			Name:        "Status fields with descriptors",
			Description: "All status fields have matching descriptors in the CSV",
			Cumulative:  true,
		},
	}
}

// Test Suite Declarations

// NewBasicTestSuite returns a new TestSuite object containing basic, functional operator tests
func NewBasicTestSuite(conf BasicTestConfig) *TestSuite {
	ts := NewTestSuite(
		"Basic Tests",
		"Test suite that runs basic, functional operator tests",
	)
	ts.AddTest(NewCheckSpecTest(conf), 1.5)
	ts.AddTest(NewCheckStatusTest(conf), 1)
	ts.AddTest(NewWritingIntoCRsHasEffectTest(conf), 1)

	return ts
}

// NewOLMTestSuite returns a new TestSuite object containing CSV best practice checks
func NewOLMTestSuite(conf OLMTestConfig) *TestSuite {
	ts := NewTestSuite(
		"OLM Tests",
		"Test suite checks if an operator's CSV follows best practices",
	)

	ts.AddTest(NewCRDsHaveValidationTest(conf), 1.25)
	ts.AddTest(NewCRDsHaveResourcesTest(conf), 1)
	ts.AddTest(NewAnnotationsContainExamplesTest(conf), 1)
	ts.AddTest(NewSpecDescriptorsTest(conf), 1)
	ts.AddTest(NewStatusDescriptorsTest(conf), 1)

	return ts
}

// Helper functions

// ResultsPassFail will be used when multiple CRs are supported
func ResultsPassFail(results []TestResult) (earned, max int) {
	for _, result := range results {
		if result.EarnedPoints != result.MaximumPoints {
			return 0, 1
		}
	}
	return 1, 1
}

// ResultsCumulative will be used when multiple CRs are supported
func ResultsCumulative(results []TestResult) (earned, max int) {
	for _, result := range results {
		earned += result.EarnedPoints
		max += result.MaximumPoints
	}
	return earned, max
}

// AddTest adds a new Test to a TestSuite along with a relative weight for the new Test
func (ts *TestSuite) AddTest(t Test, weight float64) {
	ts.Tests = append(ts.Tests, t)
	ts.Weights[t.GetName()] = weight
}

// TotalScore calculates and returns the total score of all run Tests in a TestSuite
func (ts *TestSuite) TotalScore() (score int) {
	floatScore := 0.0
	for _, result := range ts.TestResults {
		if result.MaximumPoints != 0 {
			floatScore += (float64(result.EarnedPoints) / float64(result.MaximumPoints)) * ts.Weights[result.Test.GetName()]
		}
	}
	// scale to a percentage
	addedWeights := 0.0
	for _, weight := range ts.Weights {
		addedWeights += weight
	}
	floatScore = floatScore * (100 / addedWeights)
	return int(floatScore)
}

// Run runs all Tests in a TestSuite
func (ts *TestSuite) Run(ctx context.Context) {
	for _, test := range ts.Tests {
		ts.TestResults = append(ts.TestResults, test.Run(ctx))
	}
}

// NewTestSuite returns a new TestSuite with a given name and description
func NewTestSuite(name, description string) *TestSuite {
	return &TestSuite{
		TestInfo: TestInfo{
			Name:        name,
			Description: description,
		},
		Weights: make(map[string]float64),
	}
}
