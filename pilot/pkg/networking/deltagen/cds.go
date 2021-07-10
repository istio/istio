package deltagen

import (
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/xds"
)

type DeltaCdsGenerator struct {
	Server *xds.DiscoveryServer
}

var _ model.XdsResourceGenerator = &DeltaCdsGenerator{}

func deltaCdsNeedsPush(req *model.PushRequest, proxy *model.Proxy) bool {
	// cdsNeedsPush in the ADS implementation determines if a proxy needs a push
	// by scoping to resource type. If ANY Service changes, then we would send a full
	// of all clusters push to envoy in the SOTW implementation.
	// deltaCdsNeedsPush needs to determine if a proxy needs push not only scoped to types,
	// but scoped to specific resources. If Service A affects Cluster B, then we need to push ONLY
	// cluster B. We need to push clusters that are affected by the config update.

	// for delta, it is OK to have false-positives (we determine Cluster B is affected by Service A and we send it,
	// even though it is not). We cannot have false-negatives, however, because then envoy does not have all the
	// information it needs. So if ever uncertain, just push.

	// once we receive information that a config has been updated, we need to see if envoy cares about that resource.
	// we can do this by looking at the proxies WatchedResources (keyed by type). For example, if Service A affects
	// Cluster B, and Service A changes, we know Services affect Clusters, so we determine we need to push B by looking
	// at `proxy.WatchedResources[Cluster TypeUrl]` and by trying to match Service A with a Cluster that may exist in that list.
	// If the affected Cluster exists, we know we need to push that cluster.
	// We repeat this process for every config that changes.
	/*
	Envoy will request with typeUrl=Cluster
	Maybe we push everything whenever envoy requests it?
	Use the SOTW impl for this
	PushRequest.true will send full push, false will mean incremental push

	 */
	return true
}

func (d DeltaCdsGenerator) Generate(proxy *model.Proxy, push *model.PushContext, w *model.WatchedResource, updates *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	panic("implement me")
}

