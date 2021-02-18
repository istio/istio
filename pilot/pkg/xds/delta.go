package xds

import (
	cluster "github.com/envoyproxy/go-control-plane/envoy/config/cluster/v3"
	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	route "github.com/envoyproxy/go-control-plane/envoy/config/route/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/golang/protobuf/ptypes"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"time"
)

func (s *DiscoveryServer) pushDeltaXds(con *Connection, push *model.PushContext,
	curVersion string, w *model.WatchedResource, req *model.PushRequest) error {
	if w == nil {
		return nil
	}
	gen := s.findGenerator(w.TypeUrl, con)
	if gen == nil {
		// todo shouldn't we return an error here?
		// doesnt this mean that the request that was sent had an unknown resource type?
		return nil
	}
	t0 := time.Now()
	res, err := gen.Generate(con.proxy, push, w, req)
	if err != nil || res == nil {
		if s.StatusReporter != nil {
			s.StatusReporter.RegisterEvent(con.ConID, w.TypeUrl, push.LedgerVersion)
		}
		return err
	}
	defer func() {
		recordPushTime(w.TypeUrl, time.Since(t0))
	}()

	deltaResources := convertResponseToDelta(curVersion, res)
	removedResources := generateResourceDiff(deltaResources,
		w.ResourceNames)

	deltaResp := &discovery.DeltaDiscoveryResponse{
		TypeUrl:           w.TypeUrl,
		SystemVersionInfo: curVersion,
		Nonce:             nonce(push.LedgerVersion),
		Resources:         deltaResources,
		RemovedResources:  removedResources,
	}

	if err := con.sendDelta(deltaResp); err != nil {
		recordSendError(w.TypeUrl, con.ConID, err)
		return err
	}

	if _, f := SkipLogTypes[w.TypeUrl]; !f {
		if adsLog.DebugEnabled() {
			// Add additional information to logs when debug mode enabled
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s nonce:%v version:%v",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)), deltaResp.Nonce, deltaResp.SystemVersionInfo)
		} else {
			adsLog.Infof("%s: PUSH for node:%s resources:%d size:%s",
				v3.GetShortType(w.TypeUrl), con.proxy.ID, len(res), util.ByteCount(ResourceSize(res)))
		}
	}
	return nil
}

// returns resource names that do not appear in res that do appear in subscriptions
func generateResourceDiff(resources []*discovery.Resource, subscriptions []string) []string {
	resourceNames := []string{}
	for _, res := range resources {
		resourceNames = append(resourceNames, res.Name)
	}
	// this is O(n^2) - can we make it faster?
	for i := 0; i < len(subscriptions); i++ {
		for _, resourceName := range resourceNames {
			if subscriptions[i] == resourceName {
				// found resource - remove
				// potential memory leak (?)
				subscriptions = append(subscriptions[:i], subscriptions[i+1:]...)
				i--
			}
		}
	}
	return subscriptions
}

// just for experimentation
func convertResponseToDelta(ver string, resources model.Resources) []*discovery.Resource {
	convert := []*discovery.Resource{}
	for _, r := range resources {
		var name string
		switch r.TypeUrl {
		case v3.ClusterType:
			aa := &cluster.Cluster{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		case v3.ListenerType:
			aa := &listener.Listener{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		case v3.EndpointType:
			aa := &endpoint.ClusterLoadAssignment{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.ClusterName
		case v3.RouteType:
			aa := &route.RouteConfiguration{}
			ptypes.UnmarshalAny(r, aa)
			name = aa.Name
		}
		c := &discovery.Resource{
			Name:     name,
			Version:  ver,
			Resource: r,
		}
		convert = append(convert, c)
	}
	return convert
}