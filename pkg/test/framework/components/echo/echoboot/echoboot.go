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
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"sync"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	// force registraton of factory func
	_ "istio.io/istio/pkg/test/framework/components/echo/kube"
	"istio.io/istio/pkg/test/framework/resource"
)

var _ echo.Builder = builder{}

// NewBuilder for Echo Instances.
func NewBuilder(ctx resource.Context, clusters ...resource.Cluster) echo.Builder {
	// use default kube cluster unless otherwise specified
	if len(clusters) == 0 {
		clusters = resource.Clusters{ctx.Clusters().Default()}
	}
	return builder{
		ctx:     ctx,
		configs: map[cluster.Kind][]echo.Config{},
		refs:    map[cluster.Kind][]*echo.Instance{},
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

	//errs contains a multierror for failed validation during With calls
	errs error
}

func (b builder) WithConfig(cfg echo.Config) echo.Builder {
	return b.With(nil, cfg).(builder)
}

// With adds a new Echo configuration to the Builder. When a cluster is provided in the Config, it will only be applied
// to that cluster, otherwise the Config is applied to all WithClusters. Once built, if being built for a sngle cluster,
// the instance pointer will be updated to point at the new Instance.
func (b builder) With(i *echo.Instance, cfg echo.Config) echo.Builder {
	cfg = cfg.DeepCopy()
	common.FillInDefaults(&cfg)

	targetClusters := b.clusters
	if cfg.Cluster != nil {
		targetClusters = resource.Clusters{cfg.Cluster}
	}

	deployedTo := 0
	for idx, c := range targetClusters {
		ec, ok := c.(echo.Cluster)
		if !ok {
			b.errs = multierror.Append(b.errs, fmt.Errorf("attembed to deploy to %s but it does not implement echo.Cluster", c.Name()))
			continue
		}
		if ec.CanDeploy(cfg) {
			var ref *echo.Instance
			if idx == 0 {
				// ref only applies to the first cluster deployed to
				// refs shouldn't be used when deploying to multiple targetClusters
				// TODO: should we just panic if a ref is passed in a multi-cluster context?
				ref = i
			}
			k := ec.Kind()
			perClusterConfig := cfg
			perClusterConfig.Cluster = ec
			b.configs[k] = append(b.configs[k], perClusterConfig)
			b.refs[k] = append(b.refs[k], ref)
			deployedTo++
		}
	}

	// If we didn't deploy VMs, but we don't care about VMs, we can ignore this.
	shouldSkip := b.ctx.Settings().SkipVM && cfg.DeployAsVM
	if deployedTo == 0 && !shouldSkip {
		b.errs = multierror.Append(b.errs, fmt.Errorf("no clusters were eligible for app %s", cfg.Service))
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
	if b.errs != nil {
		return nil, b.errs
	}

	m := sync.Mutex{}
	out := echo.Instances{}
	errGroup := multierror.Group{}
	// run the builder func for each kind of config in parallel
	for kind, configs := range b.configs {
		kind := kind
		configs := configs
		errGroup.Go(func() error {
			buildFunc, err := echo.GetBuilder(kind)
			if err != nil {
				return err
			}
			instances, err := buildFunc(b.ctx, configs)

			// link reference pointers
			if err := assignRefs(b.refs[kind], instances); err != nil {
				return err
			}

			// safely merge instances from all kinds of cluster into one list
			m.Lock()
			defer m.Unlock()
			out = append(out, instances...)
			return nil
		})
	}
	if err := errGroup.Wait(); err != nil {
		return nil, err
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
