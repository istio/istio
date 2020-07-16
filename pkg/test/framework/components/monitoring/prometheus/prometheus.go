package prometheus

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/monitoring"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/environment"
)

type Instance interface {
	resource.Resource
	monitoring.MetricsService
}

type Config struct {
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster

	// If true, connect to an existing prometheus rather than creating a new one
	SkipDeploy bool
}

// New returns a new instance of echo.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx, c)
	})
	return
}

// NewOrFail returns a new Prometheus instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("prometheus.NewOrFail: %v", err)
	}

	return i
}
