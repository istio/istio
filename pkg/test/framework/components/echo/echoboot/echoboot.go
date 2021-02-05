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
	"strings"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/kube"

	// force registraton of factory func
	_ "istio.io/istio/pkg/test/framework/components/echo/staticvm"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var _ echo.Builder = builder{}

// NewBuilder for Echo Instances.
func NewBuilder(ctx resource.Context, clusters ...cluster.Cluster) echo.Builder {
	// use default kube cluster unless otherwise specified
	if len(clusters) == 0 {
		clusters = cluster.Clusters{ctx.Clusters().Default()}
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
	clusters cluster.Clusters

	// configs contains configurations to be built, expanded per-cluster and grouped by cluster Kind.
	configs map[cluster.Kind][]echo.Config
	// refs contains the references to assign built Instances to.
	// The length of each refs slice should match the length of the corresponding cluster slice.
	// Only the first per-cluster entry for a given config should have a non-nil ref.
	refs map[cluster.Kind][]*echo.Instance

	// errs contains a multierror for failed validation during With calls
	errs error
}

func (b builder) WithConfig(cfg echo.Config) echo.Builder {
	return b.With(nil, cfg).(builder)
}

// With adds a new Echo configuration to the Builder. When a cluster is provided in the Config, it will only be applied
// to that cluster, otherwise the Config is applied to all WithClusters. Once built, if being built for a sngle cluster,
// the instance pointer will be updated to point at the new Instance.
func (b builder) With(i *echo.Instance, cfg echo.Config) echo.Builder {
	if b.ctx.Settings().SkipVM && cfg.DeployAsVM {
		return b
	}

	cfg = cfg.DeepCopy()
	if err := common.FillInDefaults(b.ctx, &cfg); err != nil {
		b.errs = multierror.Append(b.errs, err)
		return b
	}

	targetClusters := b.clusters
	if cfg.Cluster != nil {
		targetClusters = cluster.Clusters{cfg.Cluster}
	}

	deployedTo := 0
	for idx, c := range targetClusters {
		ec, ok := c.(echo.Cluster)
		if !ok {
			b.errs = multierror.Append(b.errs, fmt.Errorf("attembed to deploy to %s but it does not implement echo.Cluster", c.Name()))
			continue
		}
		if perClusterConfig, ok := ec.CanDeploy(cfg); ok {
			var ref *echo.Instance
			if idx == 0 {
				// ref only applies to the first cluster deployed to
				// refs shouldn't be used when deploying to multiple targetClusters
				// TODO: should we just panic if a ref is passed in a multi-cluster context?
				ref = i
			}
			perClusterConfig = perClusterConfig.DeepCopy()
			k := ec.Kind()
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
func (b builder) WithClusters(clusters ...cluster.Cluster) echo.Builder {
	next := b
	next.clusters = clusters
	return next
}

func (b builder) Build() (out echo.Instances, err error) {
	scopes.Framework.Info("=== BEGIN: Deploy echo instances ===")
	defer func() {
		if err != nil {
			scopes.Framework.Error("=== FAILED: Deploy echo instances ===")
			scopes.Framework.Error(err)
		}
	}()
	// bail early if there were issues during the configuration stage
	if b.errs != nil {
		return nil, b.errs
	}

	if err = b.deployServices(); err != nil {
		return
	}
	if out, err = b.deployInstances(); err != nil {
		return
	}

	scopes.Framework.Info("=== DONE: Deploy echo instances ===")
	return
}

// deployServices deploys the kubernetes Service to all clusters. Multicluster meshes should have "sameness"
// per cluster. This avoids concurrent writes later.
func (b builder) deployServices() error {
	services := map[string]string{}
	for _, cfgs := range b.configs {
		for _, cfg := range cfgs {
			svc, err := kube.GenerateService(cfg)
			if err != nil {
				return err
			}
			if existing, ok := services[cfg.FQDN()]; ok {
				// we've already run the generation for another echo instance's config, make sure things are the same
				if existing != svc {
					return fmt.Errorf("inconsistency in %s Service definition:\n%s", cfg.Service, cmp.Diff(existing, svc))
				}
			}
			services[cfg.FQDN()] = svc
		}
	}

	errG := multierror.Group{}
	for svcNs, svcYaml := range services {
		svcYaml := svcYaml
		ns := strings.Split(svcNs, ".")[1]
		errG.Go(func() error {
			return b.ctx.Config().ApplyYAMLNoCleanup(ns, svcYaml)
		})
	}
	return errG.Wait().ErrorOrNil()
}

func (b builder) deployInstances() (echo.Instances, error) {
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
			if err != nil {
				return err
			}

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
	if err := errGroup.Wait().ErrorOrNil(); err != nil {
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
