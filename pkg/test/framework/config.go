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

package framework

import (
	"context"
	"fmt"
	"strings"

	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/framework/resource/config"
	"istio.io/istio/pkg/test/framework/resource/config/apply"
	"istio.io/istio/pkg/test/framework/resource/config/cleanup"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
	"istio.io/istio/pkg/test/util/yml"
)

var _ config.Factory = &configFactory{}

type configFactory struct {
	ctx      resource.Context
	clusters []cluster.Cluster
	prefix   string
}

func newConfigFactory(ctx resource.Context, clusters cluster.Clusters) config.Factory {
	if len(clusters) == 0 {
		clusters = ctx.Clusters()
	}
	return &configFactory{
		ctx:      ctx,
		clusters: clusters,
	}
}

// GlobalYAMLWrites records how many YAMLs we have applied from all sources.
// Note: go tests are distinct binaries per test suite, so this is the suite level number of calls
var GlobalYAMLWrites = atomic.NewUint64(0)

func (c *configFactory) New() config.Plan {
	return &configPlan{
		configFactory: c,
		yamlText:      make(map[string][]string),
	}
}

func (c *configFactory) YAML(ns string, yamlText ...string) config.Plan {
	return c.New().YAML(ns, yamlText...)
}

func (c *configFactory) Eval(ns string, args any, yamlTemplates ...string) config.Plan {
	return c.New().Eval(ns, args, yamlTemplates...)
}

func (c *configFactory) File(ns string, filePaths ...string) config.Plan {
	return c.New().File(ns, filePaths...)
}

func (c *configFactory) EvalFile(ns string, args any, filePaths ...string) config.Plan {
	return c.New().EvalFile(ns, args, filePaths...)
}

func (c *configFactory) applyYAML(cleanupStrategy cleanup.Strategy, ns string, yamlText ...string) error {
	if len(c.prefix) == 0 {
		return c.withFilePrefix("apply").(*configFactory).applyYAML(cleanupStrategy, ns, yamlText...)
	}
	GlobalYAMLWrites.Add(uint64(len(yamlText)))

	// Convert the content to files.
	yamlFiles, err := c.ctx.WriteYAML(c.prefix, yamlText...)
	if err != nil {
		return err
	}

	g, _ := errgroup.WithContext(context.TODO())
	for _, cl := range c.clusters {
		g.Go(func() error {
			scopes.Framework.Debugf("Applying to %s to namespace %v: %s", cl.StableName(), ns, strings.Join(yamlFiles, ", "))
			if err := cl.ApplyYAMLFiles(ns, yamlFiles...); err != nil {
				return fmt.Errorf("failed applying YAML files %v to ns %s in cluster %s: %v", yamlFiles, ns, cl.Name(), err)
			}
			c.ctx.CleanupStrategy(cleanupStrategy, func() {
				scopes.Framework.Debugf("Deleting from %s: %s", cl.StableName(), strings.Join(yamlFiles, ", "))
				if err := cl.DeleteYAMLFiles(ns, yamlFiles...); err != nil {
					scopes.Framework.Errorf("failed deleting YAML files %v from ns %s in cluster %s: %v", yamlFiles, ns, cl.Name(), err)
				}
			})
			return nil
		})
	}
	return g.Wait()
}

func (c *configFactory) deleteYAML(ns string, yamlText ...string) error {
	if len(c.prefix) == 0 {
		return c.withFilePrefix("delete").(*configFactory).deleteYAML(ns, yamlText...)
	}

	// Convert the content to files.
	yamlFiles, err := c.ctx.WriteYAML(c.prefix, yamlText...)
	if err != nil {
		return err
	}

	g, _ := errgroup.WithContext(context.TODO())
	for _, c := range c.clusters {
		g.Go(func() error {
			if err := c.DeleteYAMLFiles(ns, yamlFiles...); err != nil {
				return fmt.Errorf("failed deleting YAML from cluster %s: %v", c.Name(), err)
			}
			return nil
		})
	}
	return g.Wait()
}

func (c *configFactory) withFilePrefix(prefix string) config.Factory {
	return &configFactory{
		ctx:      c.ctx,
		prefix:   prefix,
		clusters: c.clusters,
	}
}

var _ config.Plan = &configPlan{}

type configPlan struct {
	*configFactory
	yamlText map[string][]string
}

func (c *configPlan) Copy() config.Plan {
	yamlText := make(map[string][]string, len(c.yamlText))
	for k, v := range c.yamlText {
		yamlText[k] = append([]string{}, v...)
	}
	return &configPlan{
		configFactory: c.configFactory,
		yamlText:      yamlText,
	}
}

func (c *configPlan) YAML(ns string, yamlText ...string) config.Plan {
	c.yamlText[ns] = append(c.yamlText[ns], splitYAML(yamlText...)...)
	return c
}

func splitYAML(yamlText ...string) []string {
	var out []string
	for _, doc := range yamlText {
		out = append(out, yml.SplitString(doc)...)
	}
	return out
}

func (c *configPlan) File(ns string, paths ...string) config.Plan {
	yamlText, err := file.AsStringArray(paths...)
	if err != nil {
		panic(err)
	}

	return c.YAML(ns, yamlText...)
}

func (c *configPlan) Eval(ns string, args any, templates ...string) config.Plan {
	return c.YAML(ns, tmpl.MustEvaluateAll(args, templates...)...)
}

func (c *configPlan) EvalFile(ns string, args any, paths ...string) config.Plan {
	templates, err := file.AsStringArray(paths...)
	if err != nil {
		panic(err)
	}

	return c.Eval(ns, args, templates...)
}

func (c *configPlan) Apply(opts ...apply.Option) error {
	// Apply the options.
	options := apply.Options{}
	for _, o := range opts {
		o.Set(&options)
	}

	// Apply for each namespace concurrently.
	g, _ := errgroup.WithContext(context.TODO())
	for ns, y := range c.yamlText {
		g.Go(func() error {
			return c.applyYAML(options.Cleanup, ns, y...)
		})
	}

	// Wait for all each apply to complete.
	if err := g.Wait(); err != nil {
		return err
	}

	return nil
}

func (c *configPlan) ApplyOrFail(t test.Failer, opts ...apply.Option) {
	t.Helper()
	if err := c.Apply(opts...); err != nil {
		t.Fatal(err)
	}
}

func (c *configPlan) Delete() error {
	// Delete for each namespace concurrently.
	g, _ := errgroup.WithContext(context.TODO())
	for ns, y := range c.yamlText {
		g.Go(func() error {
			return c.deleteYAML(ns, y...)
		})
	}

	// Wait for all each delete to complete.
	return g.Wait()
}

func (c *configPlan) DeleteOrFail(t test.Failer) {
	t.Helper()
	if err := c.Delete(); err != nil {
		t.Fatal(err)
	}
}
