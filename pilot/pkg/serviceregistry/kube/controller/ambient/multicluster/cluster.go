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

package multicluster

import (
	"crypto/sha256"
	"fmt"
	"time"

	"go.uber.org/atomic"
	corev1 "k8s.io/api/core/v1"
	discovery "k8s.io/api/discovery/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/kubetypes"
	filter "istio.io/istio/pkg/kube/namespace"
	"istio.io/istio/pkg/log"
	"istio.io/istio/pkg/monitoring"
)

var _ krt.ResourceNamer = &Cluster{}

var (
	clusterLabel = monitoring.CreateLabel("cluster")
	timeouts     = monitoring.NewSum(
		"ambient_remote_cluster_sync_timeouts_total",
		"Number of times remote clusters took too long to sync, causing slow startup that excludes remote clusters.",
	)
)

const ClusterKRTMetadataKey = "cluster"

// ClientBuilder builds a new kube.Client from a kubeconfig. Mocked out for testing
type ClientBuilder = func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error)

type Cluster struct {
	// ID of the cluster.
	ID cluster.ID
	// Client for accessing the cluster.
	Client kube.Client

	SourceSecret types.NamespacedName

	KubeConfigSha [sha256.Size]byte
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// initialSyncTimeout is set when RunAndWait timed out
	initialSyncTimeout *atomic.Bool
	stop               chan struct{}
	initialized        *atomic.Bool
	*RemoteClusterCollections
}

type RemoteClusterCollections struct {
	namespaces     krt.Collection[*corev1.Namespace]
	pods           krt.Collection[*corev1.Pod]
	services       krt.Collection[*corev1.Service]
	endpointSlices krt.Collection[*discovery.EndpointSlice]
	nodes          krt.Collection[*corev1.Node]
	gateways       krt.Collection[*v1beta1.Gateway]
}

// Namespaces returns the namespaces collection.
func (r *RemoteClusterCollections) Namespaces() krt.Collection[*corev1.Namespace] {
	return r.namespaces
}

// Pods returns the pods collection.
func (r *RemoteClusterCollections) Pods() krt.Collection[*corev1.Pod] {
	return r.pods
}

// Services returns the services collection.
func (r *RemoteClusterCollections) Services() krt.Collection[*corev1.Service] {
	return r.services
}

// EndpointSlices returns the endpointSlices collection.
func (r *RemoteClusterCollections) EndpointSlices() krt.Collection[*discovery.EndpointSlice] {
	return r.endpointSlices
}

// Nodes returns the nodes collection.
func (r *RemoteClusterCollections) Nodes() krt.Collection[*corev1.Node] {
	return r.nodes
}

// Gateways returns the gateways collection.
func (r *RemoteClusterCollections) Gateways() krt.Collection[*v1beta1.Gateway] {
	return r.gateways
}

func NewRemoteClusterCollections(
	namespaces krt.Collection[*corev1.Namespace],
	pods krt.Collection[*corev1.Pod],
	services krt.Collection[*corev1.Service],
	endpointSlices krt.Collection[*discovery.EndpointSlice],
	nodes krt.Collection[*corev1.Node],
	gateways krt.Collection[*v1beta1.Gateway],
) *RemoteClusterCollections {
	return &RemoteClusterCollections{
		namespaces:     namespaces,
		pods:           pods,
		services:       services,
		endpointSlices: endpointSlices,
		nodes:          nodes,
		gateways:       gateways,
	}
}

func NewCluster(
	id cluster.ID,
	client kube.Client,
	source *types.NamespacedName,
	kubeConfigSha *[sha256.Size]byte,
	collections *RemoteClusterCollections,
) *Cluster {
	c := &Cluster{
		ID:                 id,
		Client:             client,
		stop:               make(chan struct{}),
		initialSync:        atomic.NewBool(false),
		initialSyncTimeout: atomic.NewBool(false),
		initialized:        atomic.NewBool(false),
	}

	if collections != nil {
		c.RemoteClusterCollections = collections
	}

	if source != nil {
		c.SourceSecret = *source
	}

	if kubeConfigSha != nil {
		c.KubeConfigSha = *kubeConfigSha
	}

	return c
}

func (c *Cluster) ResourceName() string {
	return c.ID.String()
}

func (c *Cluster) Run(localMeshConfig meshwatcher.WatcherCollection, debugger *krt.DebugHandler) {
	// Check and see if this is a local cluster or not
	if c.RemoteClusterCollections != nil {
		c.initialized.Store(true)
		log.Infof("Configuring cluster %s with existing informers", c.ID)
		syncers := []krt.Syncer{
			c.namespaces,
			c.gateways,
			c.services,
			c.nodes,
			c.endpointSlices,
			c.pods,
		}
		// Just wait for all syncers to be synced
		for _, syncer := range syncers {
			if !syncer.WaitUntilSynced(c.stop) {
				log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
				continue
			}
		}
		c.initialSync.Store(true)
		return
	}

	// We're about to start modifying the cluster, so we need to lock it
	if features.RemoteClusterTimeout > 0 {
		time.AfterFunc(features.RemoteClusterTimeout, func() {
			if !c.initialSync.Load() {
				log.Errorf("remote cluster %s failed to sync after %v", c.ID, features.RemoteClusterTimeout)
				timeouts.With(clusterLabel.Value(string(c.ID))).Increment()
			}
			c.initialSyncTimeout.Store(true)
			c.initialized.Store(true)
		})
	}

	opts := krt.NewOptionsBuilder(c.stop, fmt.Sprintf("ambient/cluster[%s]", c.ID), debugger)
	namespaces := kclient.New[*corev1.Namespace](c.Client)
	// This will start a namespace informer and wait for it to be ready. So we must start it in a go routine to avoid blocking.
	filter := filter.NewDiscoveryNamespacesFilter(namespaces, localMeshConfig, c.stop)
	kube.SetObjectFilter(c.Client, filter)
	// Register all of the informers before starting the client
	defaultFilter := kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}

	Namespaces := krt.WrapClient(namespaces, opts.With(
		append(opts.WithName("informer/Namespaces"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)
	Pods := krt.NewInformerFiltered[*corev1.Pod](c.Client, kclient.Filter{
		ObjectFilter:    c.Client.ObjectFilter(),
		ObjectTransform: kube.StripPodUnusedFields,
	}, opts.With(
		append(opts.WithName("informer/Pods"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	gatewayClient := kclient.NewDelayedInformer[*v1beta1.Gateway](c.Client, gvr.KubernetesGateway, kubetypes.StandardInformer, defaultFilter)
	Gateways := krt.WrapClient[*v1beta1.Gateway](gatewayClient, opts.With(
		append(opts.WithName("informer/Gateways"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)
	servicesClient := kclient.NewFiltered[*corev1.Service](c.Client, defaultFilter)
	Services := krt.WrapClient[*corev1.Service](servicesClient, opts.With(
		append(opts.WithName("informer/Services"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	Nodes := krt.NewInformerFiltered[*corev1.Node](c.Client, kclient.Filter{
		ObjectFilter:    c.Client.ObjectFilter(),
		ObjectTransform: kube.StripNodeUnusedFields,
	}, opts.With(
		append(opts.WithName("informer/Nodes"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	EndpointSlices := krt.NewInformerFiltered[*discovery.EndpointSlice](c.Client, kclient.Filter{
		ObjectFilter: c.Client.ObjectFilter(),
	}, opts.With(
		append(opts.WithName("informer/EndpointSlices"),
			krt.WithMetadata(krt.Metadata{
				ClusterKRTMetadataKey: c.ID,
			}),
		)...,
	)...)

	if !c.Client.RunAndWait(c.stop) {
		log.Warnf("remote cluster %s failed to sync", c.ID)
		return
	}

	c.RemoteClusterCollections = &RemoteClusterCollections{
		namespaces:     Namespaces,
		pods:           Pods,
		services:       Services,
		endpointSlices: EndpointSlices,
		nodes:          Nodes,
		gateways:       Gateways,
	}

	// Mark initialized before starting the client so that we don't have to wait for informers to sync.
	c.initialized.Store(true)

	if !c.Client.RunAndWait(c.stop) {
		log.Warnf("remote cluster %s failed to sync", c.ID)
		return
	}

	syncers := []krt.Syncer{
		c.namespaces,
		c.gateways,
		c.services,
		c.nodes,
		c.endpointSlices,
		c.pods,
	}

	for _, syncer := range syncers {
		if !syncer.WaitUntilSynced(c.stop) {
			log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
			return
		}
	}

	c.initialSync.Store(true)
}

func (c *Cluster) HasSynced() bool {
	// It could happen when a wrong credential provide, this cluster has no chance to run.
	// In this case, the `initialSyncTimeout` will never be set
	// In order not block istiod start up, check close as well.
	if c.Closed() {
		return true
	}
	return c.initialSync.Load() || c.initialSyncTimeout.Load()
}

func (c *Cluster) Closed() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

func (c *Cluster) SyncDidTimeout() bool {
	return !c.initialSync.Load() && c.initialSyncTimeout.Load()
}

// Stop closes the stop channel, if is safe to be called multi times.
func (c *Cluster) Stop() {
	select {
	case <-c.stop:
		return
	default:
		close(c.stop)
	}
}

func (c *Cluster) IsInitialized() bool {
	// If the cluster is initialized, it means that the remote collections have been created
	return c.initialized.Load()
}

func (c *Cluster) WaitUntilSynced(stop <-chan struct{}) bool {
	if c.HasSynced() {
		return true
	}

	if !kube.WaitForCacheSync(fmt.Sprintf("remote cluster %s init", c.ID), stop, c.IsInitialized) {
		log.Errorf("Timed out waiting for remote cluster %s to initialize", c.ID)
		return false
	}

	// Wait for all syncers to be synced
	for _, syncer := range []krt.Syncer{
		c.namespaces,
		c.gateways,
		c.services,
		c.nodes,
		c.endpointSlices,
		c.pods,
	} {
		if !syncer.WaitUntilSynced(stop) {
			log.Errorf("Timed out waiting for cluster %s to sync %v", c.ID, syncer)
			return false
		}
	}
	return true
}
