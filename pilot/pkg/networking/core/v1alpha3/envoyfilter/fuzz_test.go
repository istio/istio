package envoyfilter

import (
	"testing"

	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/fuzz"
)

func FuzzApplyClusterMerge(f *testing.F) {
	f.Fuzz(func(t *testing.T, patchCount int, hostname string, data []byte) {
		fg := fuzz.New(t, data)
		patches := fuzz.Slice[*networking.EnvoyFilter_EnvoyConfigObjectPatch](fg, patchCount%30)
		proxy := fuzz.Struct[*model.Proxy](fg)
		mesh := fuzz.Struct[*meshconfig.MeshConfig](fg)
		c := fuzz.Struct[*cluster.Cluster](fg)

		serviceDiscovery := memory.NewServiceDiscovery()
		env := newTestEnvironment(serviceDiscovery, mesh, buildEnvoyFilterConfigStore(patches))
		push := model.NewPushContext()
		push.InitContext(env, nil, nil)
		efw := push.EnvoyFilters(proxy)
		ApplyClusterMerge(networking.EnvoyFilter_GATEWAY, efw, c, []host.Name{host.Name(hostname)})
	})
}
