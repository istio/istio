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

package framework

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/util/sets"
	"istio.io/pkg/log"
)

type commonAnalyzer struct {
	labels                       label.Set
	minCusters, maxClusters      int
	minIstioVersion, skipMessage string
}

func newCommonAnalyzer() commonAnalyzer {
	return commonAnalyzer{
		labels:          label.NewSet(),
		skipMessage:     "",
		minIstioVersion: "",
		minCusters:      1,
		maxClusters:     -1,
	}
}

type suiteAnalyzer struct {
	testID string
	mRun   mRunFn
	osExit func(int)

	commonAnalyzer
	envFactoryCalls int
}

func newSuiteAnalyzer(testID string, fn mRunFn, osExit func(int)) Suite {
	return &suiteAnalyzer{
		testID:         testID,
		mRun:           fn,
		osExit:         osExit,
		commonAnalyzer: newCommonAnalyzer(),
	}
}

func (s *suiteAnalyzer) EnvironmentFactory(_ resource.EnvironmentFactory) Suite {
	if s.envFactoryCalls > 0 {
		scopes.Framework.Warn("EnvironmentFactory overridden multiple times for Suite")
	}
	s.envFactoryCalls++
	return s
}

func (s *suiteAnalyzer) Label(labels ...label.Instance) Suite {
	s.labels = s.labels.Add(labels...)
	return s
}

func (s *suiteAnalyzer) Skip(reason string) Suite {
	s.skipMessage = reason
	return s
}

func (s *suiteAnalyzer) SkipIf(reason string, _ resource.ShouldSkipFn) Suite {
	s.skipMessage = reason
	return s
}

func (s *suiteAnalyzer) RequireMinClusters(minClusters int) Suite {
	if minClusters <= 0 {
		minClusters = 1
	}
	s.minCusters = minClusters
	return s
}

func (s *suiteAnalyzer) RequireMaxClusters(maxClusters int) Suite {
	s.maxClusters = maxClusters
	return s
}

func (s *suiteAnalyzer) RequireSingleCluster() Suite {
	// nolint: staticcheck
	return s.RequireMinClusters(1).RequireMaxClusters(1)
}

func (s *suiteAnalyzer) RequireMultiPrimary() Suite {
	return s
}

func (s *suiteAnalyzer) SkipExternalControlPlaneTopology() Suite {
	return s
}

func (s *suiteAnalyzer) RequireExternalControlPlaneTopology() Suite {
	return s
}

func (s *suiteAnalyzer) RequireMinVersion(minorVersion uint) Suite {
	return s
}

func (s *suiteAnalyzer) RequireMaxVersion(uint) Suite {
	return s
}

func (s *suiteAnalyzer) Setup(resource.SetupFn) Suite {
	// TODO track setup fns?
	return s
}

func (s *suiteAnalyzer) SetupParallel(_ ...resource.SetupFn) Suite {
	// TODO track setup fns?
	return s
}

func (s *suiteAnalyzer) Run() {
	s.osExit(s.run())
}

func (s *suiteAnalyzer) run() int {
	initAnalysis(s.track())
	defer finishAnalysis()
	scopes.Framework.Infof("=== Begin: Analysis of %s ===", analysis.SuiteID)

	err := s.validate()
	ret := s.mRun(nil)
	if err != nil {
		scopes.Framework.Error(err)
		if ret == 0 {
			ret = 1
		}
	}

	// tests will add their results to the suiteAnalysis during mRun
	return ret
}

// track generates the final analysis for this suite. track should not be called if more
// modification will happen to this suiteAnalyzer.
func (s *suiteAnalyzer) track() *suiteAnalysis {
	return &suiteAnalysis{
		SuiteID:          s.testID,
		SkipReason:       s.skipMessage,
		Labels:           s.labels.All(),
		MultiCluster:     s.maxClusters != 1,
		MultiClusterOnly: s.minCusters > 1,
		Tests:            map[string]*testAnalysis{},
	}
}

var analyzerAllowlist = loadAllowlist(env.IstioSrc + "/pkg/test/framework/analyzer-allowlist.yaml")

type allowlist struct {
	Suites map[string][]string `yaml:"suites"`
}

func loadAllowlist(path string) *allowlist {
	out := &allowlist{Suites: map[string][]string{}}
	data, err := os.ReadFile(path)
	if err != nil {
		scopes.Framework.Warnf("failed reading suite analyzer allowlist from %s: %v", path, err)
		return out
	}
	if err := yaml.Unmarshal(data, out); err != nil {
		scopes.Framework.Warnf("failed unmarshalling suite analyzer allowlist from %s: %v", path, err)
		return out
	}
	return out
}

// validate must be called after initAnalysis/track
func (s *suiteAnalyzer) validate() error {
	var err *multierror.Error
	for name, validator := range sutieValidators {
		if sets.New(analyzerAllowlist.Suites[name]...).Contains(s.testID) {
			continue
		}
		if vErr := validator(s); vErr != nil {
			err = multierror.Append(err, vErr)
		}
	}
	// add any other suite-level validation here
	return err.ErrorOrNil()
}

var sutieValidators = map[string]func(s *suiteAnalyzer) error{
	"supportMultipleClusters": func(s *suiteAnalyzer) error {
		if s.maxClusters >= 0 && s.maxClusters < 3 {
			return fmt.Errorf(
				"%s supports a maximum of %d clusters; "+
					"suites must be compatible with an arbitrary number of clusters",
				s.testID, s.maxClusters,
			)
		}
		return nil
	},
}

func newTestAnalyzer(t *testing.T) Test {
	return &testAnalyzer{
		commonAnalyzer: newCommonAnalyzer(),
		goTest:         t,
		featureLabels:  map[features.Feature][]string{},
	}
}

type testAnalyzer struct {
	goTest *testing.T

	commonAnalyzer
	notImplemented bool
	featureLabels  map[features.Feature][]string
	hasRun         bool
}

func (t *testAnalyzer) Label(labels ...label.Instance) Test {
	t.labels = t.labels.Add(labels...)
	return t
}

func (t *testAnalyzer) Features(feats ...features.Feature) Test {
	if err := addFeatureLabels(t.featureLabels, feats...); err != nil {
		log.Errorf(err)
		t.goTest.FailNow()
	}
	return t
}

func addFeatureLabels(featureLabels map[features.Feature][]string, feats ...features.Feature) error {
	c, err := features.BuildChecker(env.IstioSrc + "/pkg/test/framework/features/features.yaml")
	if err != nil {
		return fmt.Errorf("unable to build feature checker: %v", err)
	}

	err = nil
	for _, f := range feats {
		check, scenario := c.Check(f)
		if !check {
			err = multierror.Append(err, fmt.Errorf("feature %s is not a leaf in /pkg/test/framework/features/features.yaml", f))
			continue
		}
		// feats actually contains feature and scenario.  split them here.
		onlyFeature := features.Feature(strings.Replace(string(f), scenario, "", 1))
		featureLabels[onlyFeature] = append(featureLabels[onlyFeature], scenario)
	}
	return err
}

func (t *testAnalyzer) NotImplementedYet(features ...features.Feature) Test {
	t.notImplemented = true
	t.Features(features...)
	return t
}

func (t *testAnalyzer) RequiresMinClusters(minClusters int) Test {
	t.minCusters = minClusters
	return t
}

func (t *testAnalyzer) RequiresMaxClusters(maxClusters int) Test {
	t.maxClusters = maxClusters
	return t
}

func (t *testAnalyzer) RequireIstioVersion(version string) Test {
	t.minIstioVersion = version
	return t
}

func (t *testAnalyzer) RequiresSingleCluster() Test {
	t.RequiresMinClusters(1)
	t.RequiresMaxClusters(1)
	return t
}

func (t *testAnalyzer) RequiresLocalControlPlane() Test {
	return t
}

func (t *testAnalyzer) RequiresSingleNetwork() Test {
	return t
}

func (t *testAnalyzer) Run(_ func(ctx TestContext)) {
	defer t.track()
	if t.hasRun {
		t.goTest.Fatalf("multiple Run calls for %s", t.goTest.Name())
	}
	t.hasRun = true

	// don't fail tests that would otherwise be skipped
	if analysis.SkipReason != "" || t.skipMessage != "" {
		return
	}

	// TODO: should we also block new cases?
	if len(t.featureLabels) < 1 && !features.GlobalAllowlist.Contains(analysis.SuiteID, t.goTest.Name()) {
		t.goTest.Fatalf("Detected new test %s in suite %s with no feature labels.  "+
			"See istio/istio/pkg/test/framework/features/README.md", t.goTest.Name(), analysis.SuiteID)
	}
}

func (t *testAnalyzer) RunParallel(fn func(ctx TestContext)) {
	t.Run(fn)
}

func (t *testAnalyzer) track() {
	analysis.addTest(t.goTest.Name(), &testAnalysis{
		SkipReason:       t.skipMessage,
		Labels:           t.labels.All(), // TODO should this be merged with suite labels?
		Features:         t.featureLabels,
		Invalid:          t.goTest.Failed(),
		MultiCluster:     t.maxClusters != 1 && analysis.MultiCluster,
		MultiClusterOnly: t.minCusters > 1 || analysis.MultiClusterOnly,
		NotImplemented:   t.notImplemented,
	})
}
