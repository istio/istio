// +build integ
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

package echoboot

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/resource"
)

// NewBuilder for Echo Instances.
func NewBuilder(ctx resource.Context, clusters ...resource.Cluster) Builder {
	return builder{
		kubeBuilder: kube.NewBuilder(ctx),
	}.WithClusters(clusters...)
}

// Builder is a superset of echo.Builder, which allows deploying the same echo configuration accross clusters.
type Builder interface {
	echo.Builder

	// With is the legacy version of WithConfig.
	With(i *echo.Instance, cfg echo.Config) echo.Builder

	// WithConfig mimics the behavior of With, but does not allow passing a reference
	// and returns an echoboot builder rather than a generic echo builder.
	// TODO rename this to With, and the old method to WithInstance
	WithConfig(cfg echo.Config) Builder

	// WithClusters will cause subsequent With or WithConfig calls to be applied to the given clusters.
	WithClusters(...resource.Cluster) Builder
}

var _ Builder = builder{}

type builder struct {
	clusters    resource.Clusters
	kubeBuilder echo.Builder
}

func (b builder) WithConfig(cfg echo.Config) Builder {
	return b.With(nil, cfg).(builder)
}

// With adds a new Echo configuration to the Builder. When a cluster is provided in the Config, it will only be applied
// to that cluster, otherwise the Config is applied to all WithClusters. Once built, if being built for a sngle cluster,
// the instance pointer will be updated to point at the new Instance.
func (b builder) With(i *echo.Instance, cfg echo.Config) echo.Builder {
	// TODO support for other kinds of cluster

	// Per-config cluster override, or no WithClusters provided
	if cfg.Cluster != nil || len(b.clusters) == 0 {
		b.kubeBuilder = b.kubeBuilder.With(i, cfg)
		return b
	}

	// WithClusters
	// only provide the ref for a single cluster
	var ref *echo.Instance
	if len(b.clusters) == 1 {
		ref = i
	}
	// provide a config for each cluster to the kubeBuilder
	for _, cluster := range b.clusters {
		// TODO add a mechanism to allow "claiming" a config, and not providing it to other clusters
		clusterCfg := cfg
		clusterCfg.Cluster = cluster
		b.kubeBuilder.With(ref, clusterCfg)
	}

	return b
}

// WithClusters will cause subsequent With calls to be applied to the given clusters.
func (b builder) WithClusters(clusters ...resource.Cluster) Builder {
	next := b
	next.clusters = clusters
	return next
}

func (b builder) Build() (echo.Instances, error) {
	return b.kubeBuilder.Build()
}

func (b builder) BuildOrFail(t test.Failer) echo.Instances {
	return b.kubeBuilder.BuildOrFail(t)
}
