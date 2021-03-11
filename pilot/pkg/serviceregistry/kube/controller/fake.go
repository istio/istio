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

package controller

import (
	"time"

	"k8s.io/client-go/tools/cache"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube/controller/filter"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
)

const (
	defaultFakeDomainSuffix = "company.com"
)

// FakeXdsUpdater is used to test the registry.
type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events chan FakeXdsEvent
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

func (fx *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	var id string
	if req != nil && len(req.ConfigsUpdated) > 0 {
		for key := range req.ConfigsUpdated {
			id = key.Name
		}
	}
	select {
	case fx.Events <- FakeXdsEvent{Type: "xds", ID: id}:
	default:
	}
}

func (fx *FakeXdsUpdater) ProxyUpdate(_, _ string) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "proxy"}:
	default:
	}
}

// FakeXdsEvent is used to watch XdsEvents
type FakeXdsEvent struct {
	// Type of the event
	Type string

	// The id of the event
	ID string

	// The endpoints associated with an EDS push if any
	Endpoints []*model.IstioEndpoint
}

// NewFakeXDS creates a XdsUpdater reporting events via a channel.
func NewFakeXDS() *FakeXdsUpdater {
	return &FakeXdsUpdater{
		Events: make(chan FakeXdsEvent, 100),
	}
}

func (fx *FakeXdsUpdater) EDSUpdate(_, hostname string, _ string, entry []*model.IstioEndpoint) {
	if len(entry) > 0 {
		select {
		case fx.Events <- FakeXdsEvent{Type: "eds", ID: hostname, Endpoints: entry}:
		default:
		}
	}
}

func (fx *FakeXdsUpdater) EDSCacheUpdate(_, _, _ string, entry []*model.IstioEndpoint) {
}

// SvcUpdate is called when a service port mapping definition is updated.
// This interface is WIP - labels, annotations and other changes to service may be
// updated to force a EDS and CDS recomputation and incremental push, as it doesn't affect
// LDS/RDS.
func (fx *FakeXdsUpdater) SvcUpdate(_, hostname string, _ string, _ model.Event) {
	select {
	case fx.Events <- FakeXdsEvent{Type: "service", ID: hostname}:
	default:
	}
}

func (fx *FakeXdsUpdater) Wait(et string) *FakeXdsEvent {
	for {
		select {
		case e := <-fx.Events:
			if e.Type == et {
				return &e
			}
			continue
		case <-time.After(5 * time.Second):
			return nil
		}
	}
}

// Clear any pending event
func (fx *FakeXdsUpdater) Clear() {
	wait := true
	for wait {
		select {
		case <-fx.Events:
		default:
			wait = false
		}
	}
}

type FakeControllerOptions struct {
	Client                    kubelib.Client
	NetworksWatcher           mesh.NetworksWatcher
	MeshWatcher               mesh.Watcher
	ServiceHandler            func(service *model.Service, event model.Event)
	Mode                      EndpointMode
	ClusterID                 string
	WatchedNamespaces         string
	DomainSuffix              string
	XDSUpdater                model.XDSUpdater
	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter

	// when calling from NewFakeDiscoveryServer, we wait for the aggregate cache to sync. Waiting here can cause deadlock.
	SkipCacheSyncWait bool
	Stop              chan struct{}
}

type FakeController struct {
	*Controller
}

func NewFakeControllerWithOptions(opts FakeControllerOptions) (*FakeController, *FakeXdsUpdater) {
	xdsUpdater := opts.XDSUpdater
	if xdsUpdater == nil {
		xdsUpdater = NewFakeXDS()
	}

	domainSuffix := defaultFakeDomainSuffix
	if opts.DomainSuffix != "" {
		domainSuffix = opts.DomainSuffix
	}
	if opts.Client == nil {
		opts.Client = kubelib.NewFakeClient()
	}
	if opts.MeshWatcher == nil {
		opts.MeshWatcher = mesh.NewFixedWatcher(&meshconfig.MeshConfig{})
	}

	options := Options{
		WatchedNamespaces:         opts.WatchedNamespaces, // default is all namespaces
		DomainSuffix:              domainSuffix,
		XDSUpdater:                xdsUpdater,
		Metrics:                   &model.Environment{},
		NetworksWatcher:           opts.NetworksWatcher,
		MeshWatcher:               opts.MeshWatcher,
		EndpointMode:              opts.Mode,
		ClusterID:                 opts.ClusterID,
		SyncInterval:              time.Microsecond,
		DiscoveryNamespacesFilter: opts.DiscoveryNamespacesFilter,
	}
	c := NewController(opts.Client, options)
	if opts.ServiceHandler != nil {
		c.AppendServiceHandler(opts.ServiceHandler)
	}
	c.stop = opts.Stop
	// Run in initiation to prevent calling each test
	// TODO: fix it, so we can remove `stop` channel
	go c.Run(c.stop)
	opts.Client.RunAndWait(c.stop)
	if !opts.SkipCacheSyncWait {
		// Wait for the caches to sync, otherwise we may hit race conditions where events are dropped
		cache.WaitForCacheSync(c.stop, c.HasSynced)
	}
	var fx *FakeXdsUpdater
	if x, ok := xdsUpdater.(*FakeXdsUpdater); ok {
		fx = x
	}

	return &FakeController{c}, fx
}
