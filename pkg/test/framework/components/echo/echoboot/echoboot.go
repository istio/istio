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
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/google/go-cmp/cmp"
	"github.com/hashicorp/go-multierror"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pilot/pkg/util/sets"
	"istio.io/istio/pkg/kube/inject"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/echo/kube"

	// force registraton of factory func
	_ "istio.io/istio/pkg/test/framework/components/echo/staticvm"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
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
	b := builder{
		ctx:        ctx,
		configs:    map[cluster.Kind][]echo.Config{},
		refs:       map[cluster.Kind][]*echo.Instance{},
		namespaces: map[string]namespace.Instance{},
	}
	templates, err := b.injectionTemplates()
	if err != nil {
		// deal with this when we call Build() to avoid making the NewBuilder signature unwieldy
		b.errs = multierror.Append(b.errs, fmt.Errorf("failed finding injection templates on clusters %v", err))
	}
	b.templates = templates

	return b.WithClusters(clusters...)
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
	// namespaces caches namespaces by their prefix; used for converting Static namespace from configs into actual
	// namesapces
	namespaces map[string]namespace.Instance
	// the set of injection templates for each cluster
	templates map[string]sets.Set
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

	// cache the namespace, so manually added echo.Configs can be a part of it
	b.namespaces[cfg.Namespace.Prefix()] = cfg.Namespace

	targetClusters := b.clusters
	if cfg.Cluster != nil {
		targetClusters = cluster.Clusters{cfg.Cluster}
	}

	// If we didn't deploy VMs, but we don't care about VMs, we can ignore this.
	shouldSkip := b.ctx.Settings().SkipVM && cfg.DeployAsVM
	deployedTo := 0
	for idx, c := range targetClusters {
		ec, ok := c.(echo.Cluster)
		if !ok {
			b.errs = multierror.Append(b.errs, fmt.Errorf("attembed to deploy to %s but it does not implement echo.Cluster", c.Name()))
			continue
		}
		perClusterConfig, ok := ec.CanDeploy(cfg)
		if !ok {
			continue
		}
		if !b.validateTemplates(perClusterConfig, c) {
			if c.Kind() == cluster.Kubernetes {
				scopes.Framework.Warnf("%s does not contain injection templates for %s; skipping deployment", c.Name(), perClusterConfig.FQDN())
			}
			// Don't error out when injection template missing.
			shouldSkip = true
			continue
		}

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
	return build(b)
}

// injectionTemplates lists the set of templates for each Kube cluster
func (b builder) injectionTemplates() (map[string]sets.Set, error) {
	ns := "istio-system"
	i, err := istio.Get(b.ctx)
	if err != nil {
		scopes.Framework.Infof("defaulting to istio-system namespace for injection template discovery: %v", err)
	} else {
		ns = i.Settings().SystemNamespace
	}

	out := map[string]sets.Set{}
	for _, c := range b.ctx.Clusters().Kube() {
		out[c.Name()] = sets.NewSet()
		// TODO find a place to read revision(s) and avoid listing
		cms, err := c.CoreV1().ConfigMaps(ns).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			return nil, err
		}

		// take the intersection of the templates available from each revision in this cluster
		intersection := sets.NewSet()
		for _, item := range cms.Items {
			if !strings.HasPrefix(item.Name, "istio-sidecar-injector") {
				continue
			}
			data := &inject.Config{}
			if err := yaml.Unmarshal([]byte(item.Data["config"]), data); err != nil {
				return nil, fmt.Errorf("failed parsing injection cm in %s: %v", c.Name(), err)
			}
			if data.Templates != nil {
				t := sets.NewSet()
				for name := range data.Templates {
					t.Insert(name)
				}
				// either intersection has not been set or we intersect these templates
				// with the currenet set.
				if intersection.Empty() {
					intersection = t
				} else {
					intersection = intersection.Intersection(t)
				}
			}
		}
		for name := range intersection {
			out[c.Name()].Insert(name)
		}
	}

	return out, nil
}

// build inner allows assigning to b (assignment to receiver would be ineffective)
func build(b builder) (out echo.Instances, err error) {
	scopes.Framework.Info("=== BEGIN: Deploy echo instances ===")
	defer func() {
		if err != nil {
			scopes.Framework.Error("=== FAILED: Deploy echo instances ===")
			scopes.Framework.Error(err)
		}
	}()

	// load additional configs
	for _, cfg := range *additionalConfigs {
		// swap the namespace.Static for a namespace.kube
		b, cfg.Namespace = b.getOrCreateNamespace(cfg.Namespace.Prefix())
		// register the extra config
		b = b.WithConfig(cfg).(builder)
	}

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

func (b builder) getOrCreateNamespace(prefix string) (builder, namespace.Instance) {
	ns, ok := b.namespaces[prefix]
	if ok {
		return b, ns
	}
	ns, err := namespace.New(b.ctx, namespace.Config{Prefix: prefix, Inject: true})
	if err != nil {
		b.errs = multierror.Append(b.errs, err)
	}
	b.namespaces[prefix] = ns
	return b, ns
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

// validateTemplates returns true if the templates specified by inject.istio.io/templates on the config exist on c
func (b builder) validateTemplates(config echo.Config, c cluster.Cluster) bool {
	expected := sets.NewSet()
	for _, subset := range config.Subsets {
		expected.Insert(parseList(subset.Annotations.Get(echo.SidecarInjectTemplates))...)
	}
	if b.templates == nil || b.templates[c.Name()] == nil {
		return len(expected) == 0
	}

	return b.templates[c.Name()].SupersetOf(expected)
}

func parseList(s string) []string {
	if len(strings.TrimSpace(s)) == 0 {
		return nil
	}
	items := strings.Split(s, ",")
	for i := range items {
		items[i] = strings.TrimSpace(items[i])
	}
	return items
}
