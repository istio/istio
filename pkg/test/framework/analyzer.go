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
	"strings"
	"testing"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
)

type commonAnalyzer struct {
	labels                  label.Set
	minCusters, maxClusters int
	skip                    string
}

func newCommonAnalyzer() commonAnalyzer {
	return commonAnalyzer{
		labels:      label.NewSet(),
		skip:        "",
		minCusters:  1,
		maxClusters: -1,
	}
}

type suiteAnalyzer struct {
	testID string
	mRun   mRunFn
	osExit func(int)

	commonAnalyzer
	envFactoryCalls    int
	requiredEnvVersion string
}

func newSuiteAnalyzer(testID string, fn mRunFn, osExit func(int)) Suite {
	return &suiteAnalyzer{
		testID:         testID,
		mRun:           fn,
		osExit:         osExit,
		commonAnalyzer: newCommonAnalyzer(),
	}
}

func (s *suiteAnalyzer) EnvironmentFactory(fn resource.EnvironmentFactory) Suite {
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
	s.skip = reason
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
	return s.RequireMinClusters(1).RequireMaxClusters(1)
}

func (s *suiteAnalyzer) RequireEnvironmentVersion(version string) Suite {
	s.requiredEnvVersion = version
	return s
}

func (s *suiteAnalyzer) Setup(fn resource.SetupFn) Suite {
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

	// tests will add their results to the suiteAnalysis during mRun
	return s.mRun(nil)
}

// track generates the final analysis for this suite. track should not be called if more
// modification will happen to this suiteAnalyzer.
func (s *suiteAnalyzer) track() *suiteAnalysis {
	return &suiteAnalysis{
		SuiteID:          s.testID,
		SkipReason:       s.skip,
		Labels:           s.labels.All(),
		MultiCluster:     s.maxClusters != 1,
		MultiClusterOnly: s.minCusters > 1,
		Tests:            map[string]*testAnalysis{},
	}
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
	c, err := features.BuildChecker(env.IstioSrc + "/pkg/test/framework/features/features.yaml")
	if err != nil {
		log.Errorf("Unable to build feature checker: %s", err)
		t.goTest.FailNow()
		return nil
	}
	for _, f := range feats {
		check, scenario := c.Check(f)
		if !check {
			log.Errorf("feature %s is not present in /pkg/test/framework/features/features.yaml", f)
			t.goTest.FailNow()
			return nil
		}
		// feats actually contains feature and scenario.  split them here.
		onlyFeature := features.Feature(strings.Replace(string(f), scenario, "", 1))
		t.featureLabels[onlyFeature] = append(t.featureLabels[onlyFeature], scenario)
	}
	return t
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

func (t *testAnalyzer) RequiresSingleCluster() Test {
	t.RequiresMinClusters(1)
	t.RequiresMaxClusters(1)
	return t
}

func (t *testAnalyzer) Run(_ func(ctx TestContext)) {
	defer t.track()
	if t.hasRun {
		t.goTest.Fatalf("multiple Run calls for %s", t.goTest.Name())
	}
	t.hasRun = true

	// don't fail tests that would otherwise be skipped
	if analysis.SkipReason != "" || t.skip != "" {
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
		SkipReason:       t.skip,
		Labels:           t.labels.All(), // TODO should this be merged with suite labels?
		Features:         t.featureLabels,
		Invalid:          t.goTest.Failed(),
		MultiCluster:     t.maxClusters != 1 && analysis.MultiCluster,
		MultiClusterOnly: t.minCusters > 1 || analysis.MultiClusterOnly,
		NotImplemented:   t.notImplemented,
	})
}
