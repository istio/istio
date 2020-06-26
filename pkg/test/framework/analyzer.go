package framework

import (
	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/pkg/log"
	"strings"
)

type commonAnalyzer struct {
	labels                  label.Set
	minCusters, maxClusters int
	skip                    string
}

type suiteAnalyzer struct {
	commonAnalyzer
	envFactoryCalls    int
	requiredEnvVersion string
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
	panic("implement me")
}

type testAnalyzer struct {
	commonAnalyzer
	notImplemented bool
	featureLabels  map[features.Feature][]string
}

func (t *testAnalyzer) Label(labels ...label.Instance) Test {
	t.labels = t.labels.Add(labels...)
	return t
}

func (t *testAnalyzer) Features(feats ...features.Feature) Test {
	c, err := features.BuildChecker(env.IstioSrc + "/pkg/test/framework/features/features.yaml")
	if err != nil {
		log.Errorf("Unable to build feature checker: %s", err)
		//t.goTest.FailNow()
		return nil
	}
	for _, f := range feats {
		check, scenario := c.Check(f)
		if !check {
			log.Errorf("feature %s is not present in /pkg/test/framework/features/features.yaml", f)
			//t.goTest.FailNow()
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

func (t *testAnalyzer) Run(fn func(ctx TestContext)) {
	panic("implement me")
}

func (t *testAnalyzer) RunParallel(fn func(ctx TestContext)) {
	panic("implement me")
}
