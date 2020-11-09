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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var _ resource.ConfigManager = &configManager{}

type configManager struct {
	ctx      resource.Context
	clusters []resource.Cluster
	prefix   string
}

func newConfigManager(ctx resource.Context, clusters []resource.Cluster) resource.ConfigManager {
	if len(clusters) == 0 {
		clusters = ctx.Clusters()
	}
	return &configManager{
		ctx:      ctx,
		clusters: clusters,
	}
}

func (c *configManager) ApplyYAML(ns string, yamlText ...string) error {
	if len(c.prefix) == 0 {
		return c.WithFilePrefix("apply").ApplyYAML(ns, yamlText...)
	}

	// Convert the content to files.
	yamlFiles, err := c.ctx.WriteYAML(c.prefix, yamlText...)
	if err != nil {
		return err
	}

	for _, c := range c.clusters {
		if err := c.ApplyYAMLFiles(ns, yamlFiles...); err != nil {
			return fmt.Errorf("failed applying YAML to cluster %s: %v", c.Name(), err)
		}
	}
	return nil
}

func (c *configManager) ApplyYAMLOrFail(t test.Failer, ns string, yamlText ...string) {
	err := c.ApplyYAML(ns, yamlText...)
	if err != nil {
		t.Fatal(err)
	}
}

func (c *configManager) DeleteYAML(ns string, yamlText ...string) error {
	if len(c.prefix) == 0 {
		return c.WithFilePrefix("delete").DeleteYAML(ns, yamlText...)
	}

	// Convert the content to files.
	yamlFiles, err := c.ctx.WriteYAML(c.prefix, yamlText...)
	if err != nil {
		return err
	}

	for _, c := range c.clusters {
		if err := c.DeleteYAMLFiles(ns, yamlFiles...); err != nil {
			return fmt.Errorf("failed deleting YAML from cluster %s: %v", c.Name(), err)
		}
	}
	return nil
}

func (c *configManager) DeleteYAMLOrFail(t test.Failer, ns string, yamlText ...string) {
	err := c.DeleteYAML(ns, yamlText...)
	if err != nil {
		t.Fatal(err)
	}
}

func (c *configManager) ApplyYAMLDir(ns string, configDir string) error {
	return filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if info.IsDir() {
			return nil
		}

		scopes.Framework.Debugf("Reading config file to: %v", path)
		contents, readerr := ioutil.ReadFile(path)
		if readerr != nil {
			return readerr
		}

		return c.ApplyYAML(ns, string(contents))
	})
}

func (c *configManager) DeleteYAMLDir(ns string, configDir string) error {
	return filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}

		contents, readerr := ioutil.ReadFile(path)
		if readerr != nil {
			return readerr
		}

		return c.DeleteYAML(ns, string(contents))
	})
}

func (c *configManager) WithFilePrefix(prefix string) resource.ConfigManager {
	return &configManager{
		ctx:      c.ctx,
		prefix:   prefix,
		clusters: c.clusters,
	}
}
