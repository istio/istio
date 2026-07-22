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

// Package gatewaybackend adapts active Gateway API Backend bindings to Pilot's
// runtime service model. It deliberately does not create ServiceEntry configs.
package gatewaybackend

import (
	"sort"
	"sync"

	"istio.io/istio/pilot/pkg/model"
	destinationmodel "istio.io/istio/pilot/pkg/model/destination"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/labels"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/util/sets"
)

// RuntimeBackend is the narrow integration contract between the Gateway API
// compiler and this registry. EdgeKey identifies a consumer-specific binding;
// multiple edges may intentionally share InternalName.
type RuntimeBackend = destinationmodel.RuntimeBackend

type runtimeService struct {
	service  *model.Service
	endpoint *model.IstioEndpoint
}

// Controller projects active bindings into a deduplicated runtime registry.
type Controller struct {
	bindings     krt.Collection[RuntimeBackend]
	clusterID    cluster.ID
	xdsUpdater   model.XDSUpdater
	registration krt.HandlerRegistration

	mu       sync.RWMutex
	services map[host.Name]runtimeService
	handlers model.ControllerHandlers
	model.NetworkGatewaysHandler
}

var _ model.Controller = (*Controller)(nil)
var _ model.ServiceDiscovery = (*Controller)(nil)

func NewController(bindings krt.Collection[RuntimeBackend], clusterID cluster.ID, updater model.XDSUpdater) *Controller {
	c := &Controller{
		bindings: bindings, clusterID: clusterID, xdsUpdater: updater,
		services: map[host.Name]runtimeService{},
	}
	c.registration = bindings.RegisterBatch(c.reconcile, true)
	return c
}

func (c *Controller) reconcile(_ []krt.Event[RuntimeBackend]) {
	next := make(map[host.Name]runtimeService)
	bindings := c.bindings.List()
	sort.Slice(bindings, func(i, j int) bool { return bindings[i].EdgeKey < bindings[j].EdgeKey })
	for _, b := range bindings {
		// Bindings are per consumer, while the current API has no consumer-varying
		// fields. Collapse identical runtime identities and retain them until the
		// final edge is removed.
		if _, found := next[b.InternalName]; found {
			continue
		}
		portName := b.PortName
		if portName == "" {
			portName = "backend"
		}
		p := &model.Port{Name: portName, Port: b.Port, Protocol: b.Protocol}
		svc := &model.Service{
			Attributes: model.ServiceAttributes{
				ServiceRegistry: provider.GatewayBackend,
				Name:            b.InternalName.String(), Namespace: b.Namespace,
				K8sAttributes: model.K8sAttributes{ObjectName: b.InternalName.String()},
			},
			Ports: model.PortList{p}, CreationTime: b.CreationTime,
			Hostname: b.InternalName, DefaultAddress: constants.UnspecifiedIP,
			Resolution: model.DNSLB, MeshExternal: true, ResourceVersion: b.ResourceVersion,
		}
		next[b.InternalName] = runtimeService{service: svc, endpoint: &model.IstioEndpoint{
			Addresses: []string{b.ExternalAddress}, ServicePortName: portName,
			EndpointPort: uint32(b.Port), Namespace: b.Namespace,
		}}
	}

	c.mu.Lock()
	old := c.services
	c.services = next
	c.mu.Unlock()

	keys := sets.New[host.Name]()
	for k := range old {
		keys.Insert(k)
	}
	for k := range next {
		keys.Insert(k)
	}
	for name := range keys {
		before, hadBefore := old[name]
		after, hasAfter := next[name]
		switch {
		case !hadBefore && hasAfter:
			c.notify(nil, &after, model.EventAdd)
		case hadBefore && !hasAfter:
			c.notify(&before, nil, model.EventDelete)
		case hadBefore && hasAfter && (!before.service.Equals(after.service) || !before.endpoint.Equals(after.endpoint)):
			c.notify(&before, &after, model.EventUpdate)
		}
	}
}

func (c *Controller) notify(old, current *runtimeService, event model.Event) {
	var oldSvc, currentSvc *model.Service
	latest := current
	if old != nil {
		oldSvc = old.service
		latest = old
	}
	if current != nil {
		currentSvc = current.service
		latest = current
	}
	c.handlers.NotifyServiceHandlers(oldSvc, currentSvc, event)
	if c.xdsUpdater == nil {
		return
	}
	shard := model.ShardKeyFromRegistry(c)
	c.xdsUpdater.SvcUpdate(shard, latest.service.Hostname.String(), latest.service.Attributes.Namespace, event)
	var endpoints []*model.IstioEndpoint
	if current != nil {
		endpoints = []*model.IstioEndpoint{current.endpoint}
	}
	c.xdsUpdater.EDSUpdate(shard, latest.service.Hostname.String(), latest.service.Attributes.Namespace, endpoints)
	c.xdsUpdater.ConfigUpdate(&model.PushRequest{
		ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.Service, Name: latest.service.Hostname.String(), Namespace: latest.service.Attributes.Namespace}),
		Reason:         model.NewReasonStats(model.ServiceUpdate),
	})
}

func (c *Controller) Services() []*model.Service {
	c.mu.RLock()
	defer c.mu.RUnlock()
	names := make([]string, 0, len(c.services))
	for n := range c.services {
		names = append(names, n.String())
	}
	sort.Strings(names)
	out := make([]*model.Service, 0, len(names))
	for _, n := range names {
		out = append(out, c.services[host.Name(n)].service)
	}
	return out
}

func (c *Controller) GetService(name host.Name) *model.Service {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.services[name].service
}
func (c *Controller) GetProxyServiceTargets(*model.Proxy) []model.ServiceTarget        { return nil }
func (c *Controller) GetProxyWorkloadLabels(*model.Proxy) labels.Instance              { return nil }
func (c *Controller) MCSServices() []model.MCSServiceInfo                              { return nil }
func (c *Controller) NetworkGateways() []model.NetworkGateway                          { return nil }
func (c *Controller) AppendServiceHandler(f model.ServiceHandler)                      { c.handlers.AppendServiceHandler(f) }
func (c *Controller) AppendWorkloadHandler(func(*model.WorkloadInstance, model.Event)) {}
func (c *Controller) Run(stop <-chan struct{})                                         { <-stop }
func (c *Controller) HasSynced() bool                                                  { return c.registration.HasSynced() }
func (c *Controller) Provider() provider.ID                                            { return provider.GatewayBackend }
func (c *Controller) Cluster() cluster.ID                                              { return c.clusterID }
