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
	"fmt"
	"sync"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	// force registraton of factory func
	_ "istio.io/istio/pkg/test/framework/components/echo/kube"
	// force registraton of factory func
	_ "istio.io/istio/pkg/test/framework/components/echo/staticvm"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ echo.Builder = builder{}

// NewBuilder for Echo Instances.
func NewBuilder(ctx resource.Context, clusters ...resource.Cluster) echo.Builder {
	return builder{
		ctx:     ctx,
		configs: map[cluster.Kind][]echo.Config{},
		refs:    map[cluster.Kind][]*echo.Instance{},
		// use default kube cluster unless otherwise specified
		clusters: resource.Clusters{ctx.Clusters().Default()},
	}.WithClusters(clusters...)
}

type builder struct {
	ctx resource.Context

	// clusters contains the current set of clusters that subsequent With calls will be applied to,
	// if the Config passed to With does not explicitly choose a cluster.
	clusters resource.Clusters

	// configs contains configurations to be built, expanded per-cluster and grouped by cluster Kind.
	configs map[cluster.Kind][]echo.Config
	// refs contains the references to assign built Instances to.
	// The length of each refs slice should match the length of the corresponding cluster slice.
	// Only the first per-cluster entry for a given config should have a non-nil ref.
	refs map[cluster.Kind][]*echo.Instance
}

func (b builder) WithConfig(cfg echo.Config) echo.Builder {
	return b.With(nil, cfg).(builder)
}

// TODO this is a hack, can't add the Kind() method to resource.Cluster, need to move
// the Cluster interface to the cluster package to avooid circular import
type kinder interface {
	Kind() cluster.Kind
}

// With adds a new Echo configuration to the Builder. When a cluster is provided in the Config, it will only be applied
// to that cluster, otherwise the Config is applied to all WithClusters. Once built, if being built for a sngle cluster,
// the instance pointer will be updated to point at the new Instance.
func (b builder) With(i *echo.Instance, cfg echo.Config) echo.Builder {
	clusters := b.clusters
	if cfg.Cluster != nil {
		clusters = resource.Clusters{cfg.Cluster}
	}

	// split Claimer clusters from other kinds, we check them first so they can
	// grab configs before they get propagated everywhere.
	// TODO should we implement a Kubevm cluster kind rather than the kube cluster handling fake VMs?
	claimers, others := resource.Clusters{}, resource.Clusters{}
	for _, c := range clusters {
		_, ok := c.(echo.Claimer)
		if ok {
			claimers = append(claimers, c)
		} else {
			others = append(others, c)
		}
	}

	clusters = append(claimers, others...)

	for idx, targetCluster := range clusters {
		claimer, ok := targetCluster.(echo.Claimer)
		claimed := ok
		if ok {
			cfg := cfg
			cfg.Cluster = nil
			claimed, _ := claimer.Claim([]echo.Config{cfg})
			if len(claimed) == 0 {
				continue
			}
		}

		var ref *echo.Instance
		if idx == 0 {
			// ref only applies to the first cluster deployed to
			// refs shouldn't be used when deploying to multiple clusters
			// TODO: should we just panic if a ref is passed in a multi-cluster context?
			ref = i
		}

		// TODO remove this hack after fixing Cluster interface
		clusterKinder, ok := targetCluster.(kinder)
		if !ok {
			panic(fmt.Sprintf("Cluster does not implement Kind: %v", targetCluster))
		}
		k := clusterKinder.Kind()

		perClusterConfig := cfg
		perClusterConfig.Cluster = targetCluster
		b.configs[k] = append(b.configs[k], perClusterConfig)
		b.refs[k] = append(b.refs[k], ref)

		if claimed {
			break
		}
	}

	return b
}

// WithClusters will cause subsequent With calls to be applied to the given clusters.
func (b builder) WithClusters(clusters ...resource.Cluster) echo.Builder {
	next := b
	next.clusters = clusters
	return next
}

func (b builder) Build() (echo.Instances, error) {
	m := sync.Mutex{}
	results := map[cluster.Kind]echo.Instances{}
	errGroup := multierror.Group{}
	for k, configs := range b.configs {
		k := k
		configs := configs
		errGroup.Go(func() error {
			f, err := echo.GetFactory(k)
			if err != nil {
				return err
			}

			instances, err := f(b.ctx, configs)
			if err := assignRefs(b.refs[k], instances); err != nil {
				return err
			}

			m.Lock()
			defer m.Unlock()
			results[k] = append(results[k], instances...)
			return nil
		})
	}
	if err := errGroup.Wait(); err != nil {
		return nil, err
	}

	out := echo.Instances{}
	for _, instances := range results {
		out = append(out, instances...)
	}
	return out, nil
}

func assignRefs(refs []*echo.Instance, instances echo.Instances) error {
	if len(refs) != len(instances) {
		return fmt.Errorf("cannot set %d references, only %d instances were built", len(refs), len(instances))
	}
	for i, ref := range refs {
		if ref != nil {
			*ref = instances[i]
		}
	}
	return nil
}

func (b builder) BuildOrFail(t test.Failer) echo.Instances {
	t.Helper()
	out, err := b.Build()
	if err != nil {
		t.Fatal(err)
	}
	return out
}
