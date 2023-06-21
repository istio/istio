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

	"k8s.io/apimachinery/pkg/runtime/schema"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	defaultFakeDomainSuffix = "company.com"
)

type FakeControllerOptions struct {
	Client                    kubelib.Client
	CRDs                      []schema.GroupVersionResource
	NetworksWatcher           mesh.NetworksWatcher
	MeshWatcher               mesh.Watcher
	ServiceHandler            model.ServiceHandler
	ClusterID                 cluster.ID
	WatchedNamespaces         string
	DomainSuffix              string
	XDSUpdater                model.XDSUpdater
	DiscoveryNamespacesFilter filter.DiscoveryNamespacesFilter
	Stop                      chan struct{}
	SkipRun                   bool
	ConfigController          model.ConfigStoreController
	ConfigCluster             bool
	WorkloadEntryEnabled      bool
}

type FakeController struct {
	*Controller
}

func NewFakeControllerWithOptions(t test.Failer, opts FakeControllerOptions) (*FakeController, *xdsfake.Updater) {
	xdsUpdater := opts.XDSUpdater
	if xdsUpdater == nil {
		xdsUpdater = xdsfake.NewFakeXDS()
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

	meshServiceController := aggregate.NewController(aggregate.Options{MeshHolder: opts.MeshWatcher})

	options := Options{
		DomainSuffix:              domainSuffix,
		XDSUpdater:                xdsUpdater,
		Metrics:                   &model.Environment{},
		MeshNetworksWatcher:       opts.NetworksWatcher,
		MeshWatcher:               opts.MeshWatcher,
		ClusterID:                 opts.ClusterID,
		DiscoveryNamespacesFilter: opts.DiscoveryNamespacesFilter,
		MeshServiceController:     meshServiceController,
		ConfigCluster:             opts.ConfigCluster,
		ConfigController:          opts.ConfigController,
	}
	c := NewController(opts.Client, options)
	meshServiceController.AddRegistry(c)

	if opts.ServiceHandler != nil {
		c.AppendServiceHandler(opts.ServiceHandler)
	}

	t.Cleanup(func() {
		c.client.Shutdown()
	})
	if !opts.SkipRun {
		t.Cleanup(func() {
			assert.NoError(t, queue.WaitForClose(c.queue, time.Second*5))
		})
	}
	c.stop = opts.Stop
	if c.stop == nil {
		// If we created the stop, clean it up. Otherwise, caller is responsible
		c.stop = test.NewStop(t)
	}
	for _, crd := range opts.CRDs {
		clienttest.MakeCRD(t, c.client, crd)
	}
	opts.Client.RunAndWait(c.stop)
	var fx *xdsfake.Updater
	if x, ok := xdsUpdater.(*xdsfake.Updater); ok {
		fx = x
	}

	if !opts.SkipRun {
		go c.Run(c.stop)
		kubelib.WaitForCacheSync("test", c.stop, c.HasSynced)
	}

	return &FakeController{c}, fx
}
