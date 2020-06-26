package framework

import (
	"istio.io/istio/pkg/test/framework/features"
	"istio.io/istio/pkg/test/framework/label"
	"istio.io/istio/pkg/test/framework/resource"
)

type suiteAnalyzer struct {
	
}

func (s *suiteAnalyzer) EnvironmentFactory(fn resource.EnvironmentFactory) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) Label(labels ...label.Instance) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) Skip(reason string) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) RequireMinClusters(minClusters int) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) RequireMaxClusters(maxClusters int) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) RequireSingleCluster() Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) RequireEnvironmentVersion(version string) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) Setup(fn resource.SetupFn) Suite {
	panic("implement me")
}

func (s *suiteAnalyzer) Run() {
	panic("implement me")
}

type testAnalyzer struct {

}

func (t *testAnalyzer) Label(labels ...label.Instance) Test {
	panic("implement me")
}

func (t *testAnalyzer) Features(feats ...features.Feature) Test {
	panic("implement me")
}

func (t *testAnalyzer) NotImplementedYet(features ...features.Feature) Test {
	panic("implement me")
}

func (t *testAnalyzer) RequiresMinClusters(minClusters int) Test {
	panic("implement me")
}

func (t *testAnalyzer) RequiresMaxClusters(maxClusters int) Test {
	panic("implement me")
}

func (t *testAnalyzer) RequiresSingleCluster(maxClusters int) Test {
	panic("implement me")
}

func (t *testAnalyzer) Run(fn func(ctx TestContext)) {
	panic("implement me")
}

func (t *testAnalyzer) RunParallel(fn func(ctx TestContext)) {
	panic("implement me")
}

