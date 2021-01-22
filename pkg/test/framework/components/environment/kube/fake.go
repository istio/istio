package kube

import (
	"fmt"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/cluster/aggregate"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ resource.Environment = FakeEnvironment{}

// FakeEnvironment for testing.
type FakeEnvironment struct {
	Name        string
	NumClusters int
	IDValue     string
}

func (f FakeEnvironment) ID() resource.ID {
	return resource.FakeID(f.IDValue)
}

func (f FakeEnvironment) IsMultinetwork() bool {
	return false
}

func (f FakeEnvironment) EnvironmentName() string {
	if len(f.Name) == 0 {
		return "fake"
	}
	return f.Name
}

func (f FakeEnvironment) Clusters() resource.Clusters {
	factory := aggregate.NewFactory()
	for i := 0; i < f.NumClusters; i++ {
		factory = factory.With(cluster.Config{Kind: cluster.Fake, Name: fmt.Sprintf("cluster-%d", i)})
	}
	out, err := factory.Build(nil)
	if err != nil {
		panic(err)
	}
	return out
}
