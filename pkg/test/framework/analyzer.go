package framework

import (
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
	"strings"
	"testing"
)

type commonAnalyzer struct {
	labels                  label.Set
	minCusters, maxClusters int
	skip                    string
}

func newCommonAnalyzer() commonAnalyzer {
	return commonAnalyzer{
		labels:      label.NewSet(),
		minCusters:  -1,
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
	s.minCusters = minClusters
	return s
}

func (s *suiteAnalyzer) RequireMaxClusters(maxClusters int) Suite {
	s.maxClusters = maxClusters
	return s
}

func (s *suiteAnalyzer) RequireSingleCluster() Suite {
	s.RequireMinClusters(1)
	s.RequireMaxClusters(1)
	return s
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
	initAnalysis(&suiteAnalysis{
		ID: s.testID,
		// TODO track other info
	})
	defer finishAnalysis()

	// during mRun tests will add their analyses to the suiteAnalysis
	return s.mRun(nil)
}

//////////////////////////////////////////////////////////////////////////////////////////

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
	//t.Run(func(_ TestContext) { t.goTest.Skip("Test Not Yet Implemented") })
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
	if t.hasRun {
		t.goTest.Fatalf("multiple Run calls for %s", t.goTest.Name())
	}
	t.hasRun = true
	analysis.addTest(&testAnalysis{
		ID: t.goTest.Name(),
		// TODO track other info
	})
}

func (t *testAnalyzer) RunParallel(fn func(ctx TestContext)) {
	t.Run(fn)
}
