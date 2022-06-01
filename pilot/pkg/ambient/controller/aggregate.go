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
	"sync"

	"istio.io/istio/pilot/pkg/ambient"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/kube/multicluster"
	istiolog "istio.io/pkg/log"
)

var log = istiolog.RegisterScope("ambient", "ambient mesh controllers", 0)

type Options struct {
	xds       model.XDSUpdater
	Client    kubelib.Client
	Stop      <-chan struct{}
	ClusterID cluster.ID

	SystemNamespace string

	LocalCluster  bool
	WebhookConfig func() inject.WebhookConfig
}

var (
	_ multicluster.ClusterHandler = &Aggregate{}
	_ ambient.Cache               = &Aggregate{}
)

func NewAggregate(
	systemNamespace string,
	localCluster cluster.ID,
	webhookConfig func() inject.WebhookConfig,
	xdsUpdater model.XDSUpdater,
) *Aggregate {
	return &Aggregate{
		localCluster: localCluster,
		baseOpts: Options{
			SystemNamespace: systemNamespace,
			WebhookConfig:   webhookConfig,
			xds:             xdsUpdater,
		},

		clusters: make(map[cluster.ID]*ambientController),
	}
}

type Aggregate struct {
	localCluster cluster.ID
	baseOpts     Options

	mu       sync.RWMutex
	clusters map[cluster.ID]*ambientController
}

func (a *Aggregate) SidecarlessWorkloads() ambient.Indexes {
	out := ambient.Indexes{
		Workloads: ambient.NewWorkloadIndex(),
		PEPs:      ambient.NewWorkloadIndex(),
		UProxies:  ambient.NewWorkloadIndex(),
		None:      ambient.NewWorkloadIndex(),
	}

	// consistent ordering should be handled somewhere (config gen, workload index), but not in the cluster iteration
	a.mu.RLock()
	defer a.mu.RUnlock()
	for _, c := range a.clusters {
		ci := c.workloads.SidecarlessWorkloads()
		ci.Workloads.MergeInto(out.Workloads)
		ci.None.MergeInto(out.None)
		ci.PEPs.MergeInto(out.PEPs)
		ci.UProxies.MergeInto(out.UProxies)
	}
	return out
}

func (a *Aggregate) ClusterAdded(cluster *multicluster.Cluster, stop <-chan struct{}) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.clusterAdded(cluster, stop)
}

func (a *Aggregate) clusterAdded(cluster *multicluster.Cluster, stop <-chan struct{}) error {
	log.Infof("starting ambient controller for %s", cluster.ID)
	opts := a.baseOpts

	opts.Client = cluster.Client
	opts.Stop = stop
	opts.ClusterID = cluster.ID

	// don't modify remote clusters, just find their PEPs and Pods
	opts.LocalCluster = a.localCluster == cluster.ID

	a.clusters[cluster.ID] = initForCluster(&opts)
	return nil
}

func initForCluster(opts *Options) *ambientController {
	if opts.LocalCluster {
		// TODO handle istiodless remote clusters
		initAutolabel(opts)
		remoteProxy := NewRemoteProxyController(opts.Client, opts.ClusterID, opts.WebhookConfig)
		go remoteProxy.Run(opts.Stop)
	}
	return &ambientController{
		workloads: initWorkloadCache(opts),
	}
}

func (a *Aggregate) ClusterUpdated(cluster *multicluster.Cluster, stop <-chan struct{}) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.clusterDeleted(cluster.ID); err != nil {
		return err
	}
	return a.clusterAdded(cluster, stop)
}

func (a *Aggregate) ClusterDeleted(clusterID cluster.ID) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.clusterDeleted(clusterID)
}

func (a *Aggregate) clusterDeleted(clusterID cluster.ID) error {
	delete(a.clusters, clusterID)
	return nil
}

type ambientController struct {
	workloads *workloadCache
}
