package deltagen

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
)

type DeltaCdsGenerator struct {
	Server *xds.DiscoveryServer
}

var _ model.XdsResourceGenerator = &DeltaCdsGenerator{}

// Generate for delta. it is OK to have false-positives (we determine Cluster B is affected by Service A and we send it,
// even though it is not). We cannot have false-negatives, however, because then envoy does not have all the
// information it needs. So if ever uncertain, just push.
// once we receive information that a config has been updated, we need to see if envoy cares about that resource.
// we can do this by looking at the proxies WatchedResources (keyed by type). For example, if Service A affects
// Cluster B, and Service A changes, we know Services affect Clusters, so we determine we need to push B by looking
// at `proxy.WatchedResources[Cluster TypeUrl]` and by trying to match Service A with a Cluster that may exist in that list.
// If the affected Cluster exists, we know we need to push that cluster.
// We repeat this process for every config that changes.
func (d DeltaCdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates *model.PushRequest, delta bool) (model.Resources, model.XdsLogDetails, error) {
	// todo -- right now, we push some clusters (inbound for sidecars, passthrough and blackhole for sidecars and gateways) regardless.
	// 	therefore, it doesn't make sense to check if we need to push on a per-resource basis, for now, scoping to a per-type basis should
	// 	be a good first step.
}
