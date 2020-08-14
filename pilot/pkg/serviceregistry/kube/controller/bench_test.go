package controller

import (
	"fmt"
	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/pkg/log"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
	"testing"
)

func init() {
	for _, s := range log.Scopes() {
		s.SetOutputLevel(log.NoneLevel)
	}
}

// fakeObjects creates 5 services with 5 pods each
func fakeObjects() []runtime.Object {
	svcs, pods := 100, 200
	var out []runtime.Object
	for i := 0; i < svcs; i++ {
		var ips []string
		for j := 0; j < pods/svcs; j++ {
			ips = append(ips, fmt.Sprintf("10.10.%d.%d", i, j))
		}
		out = append(out, FakePodService(FakeServiceOpts{
			Name:      string('a' + i),
			Namespace: "fake",
			PodIPs:    ips,
		})...)
	}
	return out
}

func BenchmarkController_SyncAll(b *testing.B) {
	stop := make(chan struct{})
	defer close(stop)
	c, _ := NewFakeControllerWithOptions(FakeControllerOptions{
		Objects:         fakeObjects(),
		NetworksWatcher: mesh.NewFixedNetworksWatcher(&meshconfig.MeshNetworks{}),
		Mode:            EndpointsOnly,
		ClusterID:       "Kubernetes",
		DomainSuffix:    "cluster.local",
	})
	cache.WaitForCacheSync(stop, c.HasSynced)
	for c.queue.Len() > 0 {
	}

	cases := map[string]struct {
		store   cache.Store
		handler func(interface{}, model.Event) error
	}{
		//"node":     {c.nodeInformer.GetStore(), c.onNodeEvent},
		"service":  {c.serviceInformer.GetStore(), c.onServiceEvent},
		//"pod":      {c.pods.informer.GetStore(), c.pods.onEvent},
		//"endpoint": {c.endpoints.getInformer().GetStore(), c.endpoints.onEvent},
	}

	b.ResetTimer()
	for name, tt := range cases {
		b.Run(name, func(b *testing.B) {
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				syncResources(tt.store, tt.handler)
			}
		})
	}

}
