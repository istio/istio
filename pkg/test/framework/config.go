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

	"github.com/hashicorp/go-multierror"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/istioctl"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/file"
	"istio.io/istio/pkg/test/util/tmpl"
)

var _ resource.ConfigManager = &configManager{}

type configManager struct {
	ctx      resource.Context
	clusters []cluster.Cluster
	prefix   string
}

func newConfigManager(ctx resource.Context, clusters cluster.Clusters) resource.ConfigManager {
	if len(clusters) == 0 {
		clusters = ctx.Clusters()
	}
	return &configManager{
		ctx:      ctx,
		clusters: clusters.Kube(),
	}
}

// GlobalYAMLWrites records how many YAMLs we have applied from all sources.
// Note: go tests are distinct binaries per test suite, so this is the suite level number of calls
var GlobalYAMLWrites = atomic.NewUint64(0)

func (c *configManager) New() resource.Config {
	return &configImpl{
		configManager: c,
		yamlText:      make(map[string][]string),
	}
}

func (c *configManager) YAML(ns string, yamlText ...string) resource.Config {
	return c.New().YAML(ns, yamlText...)
}

func (c *configManager) Eval(ns string, args interface{}, yamlTemplates ...string) resource.Config {
	return c.New().Eval(ns, args, yamlTemplates...)
}

func (c *configManager) File(ns string, filePaths ...string) resource.Config {
	return c.New().File(ns, filePaths...)
}

func (c *configManager) EvalFile(ns string, args interface{}, filePaths ...string) resource.Config {
	return c.New().EvalFile(ns, args, filePaths...)
}

func (c *configManager) applyYAML(cleanup bool, ns string, yamlText ...string) error {
	if len(c.prefix) == 0 {
		return c.WithFilePrefix("apply").(*configManager).applyYAML(cleanup, ns, yamlText...)
	}
	GlobalYAMLWrites.Add(uint64(len(yamlText)))

	// Convert the content to files.
	yamlFiles, err := c.ctx.WriteYAML(c.prefix, yamlText...)
	if err != nil {
		return err
	}

	g, _ := errgroup.WithContext(context.TODO())
	for _, cl := range c.clusters {
		cl := cl
		g.Go(func() error {
			scopes.Framework.Debugf("Applying to %s to namespace %v: %s", cl.StableName(), ns, strings.Join(yamlFiles, ", "))
			if err := cl.ApplyYAMLFiles(ns, yamlFiles...); err != nil {
				return fmt.Errorf("failed applying YAML files %v to ns %s in cluster %s: %v", yamlFiles, ns, cl.Name(), err)
			}
			if cleanup {
				c.ctx.Cleanup(func() {
					scopes.Framework.Debugf("Deleting from %s: %s", cl.StableName(), strings.Join(yamlFiles, ", "))
					if err := cl.DeleteYAMLFiles(ns, yamlFiles...); err != nil {
						scopes.Framework.Errorf("failed deleting YAML files %v from ns %s in cluster %s: %v", yamlFiles, ns, cl.Name(), err)
					}
				})
			}
			return nil
		})
	}
	return g.Wait()
}

func (c *configManager) deleteYAML(ns string, yamlText ...string) error {
	if len(c.prefix) == 0 {
		return c.WithFilePrefix("delete").(*configManager).deleteYAML(ns, yamlText...)
	}

	// Convert the content to files.
	yamlFiles, err := c.ctx.WriteYAML(c.prefix, yamlText...)
	if err != nil {
		return err
	}

	g, _ := errgroup.WithContext(context.TODO())
	for _, c := range c.clusters {
		c := c
		g.Go(func() error {
			if err := c.DeleteYAMLFiles(ns, yamlFiles...); err != nil {
				return fmt.Errorf("failed deleting YAML from cluster %s: %v", c.Name(), err)
			}
			return nil
		})
	}
	return g.Wait()
}

func (c *configManager) WaitForConfig(ctx resource.Context, ns string, yamlText ...string) error {
	var outErr error
	for _, c := range c.ctx.Clusters() {
		ik, err := istioctl.New(ctx, istioctl.Config{Cluster: c})
		if err != nil {
			return err
		}

		for _, config := range yamlText {
			config := config

			// TODO(https://github.com/istio/istio/issues/37324): It's currently unsafe
			// to call istioctl concurrently since it relies on the istioctl library
			// (rather than calling the binary from the command line) which uses a number
			// of global variables, which will be overwritten for each call.
			if err := ik.WaitForConfig(ns, config); err != nil {
				// Get proxy status for additional debugging
				s, _, _ := ik.Invoke([]string{"ps"})
				outErr = multierror.Append(err, fmt.Errorf("failed waiting for config for cluster %s: err=%v. Proxy status: %v",
					c.StableName(), err, s))
			}
		}
	}
	return outErr
}

func (c *configManager) WaitForConfigOrFail(ctx resource.Context, t test.Failer, ns string, yamlText ...string) {
	err := c.WaitForConfig(ctx, ns, yamlText...)
	if err != nil {
		// TODO(https://github.com/istio/istio/issues/37148) fail hard in this case
		t.Log(err)
	}
}

func (c *configManager) WithFilePrefix(prefix string) resource.ConfigManager {
	return &configManager{
		ctx:      c.ctx,
		prefix:   prefix,
		clusters: c.clusters,
	}
}

var _ resource.Config = &configImpl{}

type configImpl struct {
	*configManager
	yamlText map[string][]string
}

func (c *configImpl) YAML(ns string, yamlText ...string) resource.Config {
	c.yamlText[ns] = append(c.yamlText[ns], yamlText...)
	return c
}

func (c *configImpl) File(ns string, paths ...string) resource.Config {
	yamlText, err := file.AsStringArray(paths...)
	if err != nil {
		panic(err)
	}

	return c.YAML(ns, yamlText...)
}

func (c *configImpl) Eval(ns string, args interface{}, templates ...string) resource.Config {
	return c.YAML(ns, tmpl.MustEvaluateAll(args, templates...)...)
}

func (c *configImpl) EvalFile(ns string, args interface{}, paths ...string) resource.Config {
	templates, err := file.AsStringArray(paths...)
	if err != nil {
		panic(err)
	}

	return c.Eval(ns, args, templates...)
}

func (c *configImpl) Apply(opts ...resource.ConfigOption) error {
	// Apply the options.
	options := resource.ConfigOptions{}
	for _, o := range opts {
		o(&options)
	}

	// Apply for each namespace concurrently.
	g, _ := errgroup.WithContext(context.TODO())
	for ns, y := range c.yamlText {
		ns, y := ns, y
		g.Go(func() error {
			return c.applyYAML(!options.NoCleanup, ns, y...)
		})
	}

	// Wait for all each apply to complete.
	if err := g.Wait(); err != nil {
		return err
	}

	if options.Wait {
		// TODO: wait for each namespace concurrently once WaitForConfig supports concurrency.
		for ns, y := range c.yamlText {
			if err := c.WaitForConfig(c.ctx, ns, y...); err != nil {
				// TODO(https://github.com/istio/istio/issues/37148) fail hard in this case
				scopes.Framework.Warnf("(Ignored until https://github.com/istio/istio/issues/37148 is fixed) "+
					"failed waiting for YAML %v: %v", y, err)
			}
		}
	}
	return nil
}

func (c *configImpl) ApplyOrFail(t test.Failer, opts ...resource.ConfigOption) {
	t.Helper()
	if err := c.Apply(opts...); err != nil {
		t.Fatal(err)
	}
}

func (c *configImpl) Delete() error {
	// Delete for each namespace concurrently.
	g, _ := errgroup.WithContext(context.TODO())
	for ns, y := range c.yamlText {
		ns, y := ns, y
		g.Go(func() error {
			return c.deleteYAML(ns, y...)
		})
	}

	// Wait for all each delete to complete.
	return g.Wait()
}

func (c *configImpl) DeleteOrFail(t test.Failer) {
	t.Helper()
	if err := c.Delete(); err != nil {
		t.Fatal(err)
	}
}
