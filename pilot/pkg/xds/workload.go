// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package xds

import (
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/config/schema/kind"
)

type WorkloadGenerator struct {
	s *DiscoveryServer
}

var (
	_ model.XdsResourceGenerator      = &WorkloadGenerator{}
	_ model.XdsDeltaResourceGenerator = &WorkloadGenerator{}
)

func (e WorkloadGenerator) GenerateDeltas(
	proxy *model.Proxy,
	req *model.PushRequest,
	w *model.WatchedResource,
) (model.Resources, model.DeletedResources, model.XdsLogDetails, bool, error) {
	// TODO: wdsNeedsPush
	resources := make(model.Resources, 0)
	// Workload consists of two dependencies: Pod and Service
	// For now, we optimize Pod updates but not Service
	// As long as there are no Service updates, we will do a delta push
	pods := model.ConfigsOfKind(req.ConfigsUpdated, kind.Pod)
	svcs := model.ConfigsOfKind(req.ConfigsUpdated, kind.Service)
	if len(req.ConfigsUpdated) == 0 {
		pods = nil
	}
	if len(svcs) > 0 {
		pods = nil
	}
	usedDelta := pods != nil
	wls, removed := e.s.Env.ServiceDiscovery.PodInformation(pods)
	for _, wl := range wls {
		resources = append(resources, &discovery.Resource{
			Name:     wl.ResourceName(),
			Resource: protoconv.MessageToAny(wl),
		})
	}
	return resources, removed, model.XdsLogDetails{}, usedDelta, nil
}

func (e WorkloadGenerator) Generate(proxy *model.Proxy, w *model.WatchedResource, req *model.PushRequest) (model.Resources, model.XdsLogDetails, error) {
	rr := &model.PushRequest{
		Full:           req.Full,
		ConfigsUpdated: nil,
		Push:           req.Push,
		Start:          req.Start,
		Reason:         req.Reason,
		Delta:          req.Delta,
	}
	res, deleted, log, usedDelta, err := e.GenerateDeltas(proxy, rr, w)
	if len(deleted) > 0 || usedDelta {
		panic("invalid delta usage")
	}
	return res, log, err
}
