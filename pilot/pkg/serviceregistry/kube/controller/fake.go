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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pkg/activenotifier"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/queue"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	defaultFakeDomainSuffix = "company.com"
)

type FakeControllerOptions struct {
	Client            kubelib.Client
	CRDs              []schema.GroupVersionResource
	NetworksWatcher   mesh.NetworksWatcher
	MeshWatcher       meshwatcher.WatcherCollection
	ServiceHandler    model.ServiceHandler
	ClusterID         cluster.ID
	WatchedNamespaces string
	DomainSuffix      string
	XDSUpdater        model.XDSUpdater
	Stop              chan struct{}
	SkipRun           bool
	ConfigCluster     bool
	SystemNamespace   string
}

type FakeController struct {
	*Controller
	Endpoints *model.EndpointIndex
}

func NewFakeControllerWithOptions(t test.Failer, opts FakeControllerOptions) (*FakeController, *xdsfake.Updater) {
	xdsUpdater := opts.XDSUpdater
	var endpoints *model.EndpointIndex
	if xdsUpdater == nil {
		endpoints = model.NewEndpointIndex(model.DisabledCache{})
		delegate := model.NewEndpointIndexUpdater(endpoints)
		xdsUpdater = xdsfake.NewWithDelegate(delegate)
	}

	domainSuffix := defaultFakeDomainSuffix
	if opts.DomainSuffix != "" {
		domainSuffix = opts.DomainSuffix
	}
	if opts.Client == nil {
		opts.Client = kubelib.NewFakeClient()
	}
	if opts.ClusterID == "" {
		opts.ClusterID = opts.Client.ClusterID()
	}
	if opts.MeshWatcher == nil {
		opts.MeshWatcher = meshwatcher.NewTestWatcher(&meshconfig.MeshConfig{TrustDomain: "cluster.local"})
	}
	cleanupStop := false
	stop := opts.Stop
	if stop == nil {
		// If we created the stop, clean it up. Otherwise, caller is responsible
		cleanupStop = true
		stop = make(chan struct{})
	}
	f := namespace.NewDiscoveryNamespacesFilter(
		kclient.New[*corev1.Namespace](opts.Client),
		opts.MeshWatcher,
		stop,
	)
	kubelib.SetObjectFilter(opts.Client, f)

	var configCluster cluster.ID
	if opts.ConfigCluster {
		configCluster = opts.ClusterID
	}
	meshServiceController := aggregate.NewController(aggregate.Options{
		MeshHolder:      opts.MeshWatcher,
		ConfigClusterID: configCluster,
	})

	options := Options{
		DomainSuffix:          domainSuffix,
		XDSUpdater:            xdsUpdater,
		Metrics:               &model.Environment{},
		MeshNetworksWatcher:   opts.NetworksWatcher,
		MeshWatcher:           opts.MeshWatcher,
		ClusterID:             opts.ClusterID,
		MeshServiceController: meshServiceController,
		ConfigCluster:         opts.ConfigCluster,
		SystemNamespace:       opts.SystemNamespace,
		StatusWritingEnabled:  activenotifier.New(false),
		KrtDebugger:           new(krt.DebugHandler),
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
	c.stop = stop
	if cleanupStop {
		t.Cleanup(func() {
			close(stop)
		})
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

	return &FakeController{Controller: c, Endpoints: endpoints}, fx
}
