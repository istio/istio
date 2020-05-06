//  Copyright Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package native

import (
	"io/ioutil"
	"os"
	"path/filepath"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/yml"
)

var _ resource.Cluster = &Cluster{}

type Cluster struct {
	// The folder that Galley reads to local, file-based configuration from
	configDir string
	cache     *yml.Cache
}

func NewCluster(ctx resource.Context) (resource.Cluster, error) {
	configDir, err := ctx.CreateTmpDirectory("config")
	if err != nil {
		return Cluster{}, err
	}
	c := Cluster{
		configDir: configDir,
		cache:     yml.NewCache(configDir),
	}
	return c, nil
}

func (c Cluster) GetConfigDir() string {
	return c.configDir
}

func (c Cluster) ApplyConfig(ns string, yamlText ...string) error {
	for _, y := range yamlText {
		y, err := yml.ApplyNamespace(y, ns)
		if err != nil {
			return err
		}

		if _, err = c.cache.Apply(y); err != nil {
			return err
		}
	}
	return nil
}

func (c Cluster) ApplyConfigOrFail(t test.Failer, ns string, yamlText ...string) {
	t.Helper()
	err := c.ApplyConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("ApplyConfigOrFail: %v", err)
	}
}

func (c Cluster) DeleteConfig(ns string, yamlText ...string) error {
	var err error
	for _, y := range yamlText {
		y, err = yml.ApplyNamespace(y, ns)
		if err != nil {
			return err
		}

		if err = c.cache.Delete(y); err != nil {
			return err
		}
	}

	return nil
}

func (c Cluster) DeleteConfigOrFail(t test.Failer, ns string, yamlText ...string) {
	t.Helper()
	err := c.DeleteConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("DeleteConfigOrFail: %v", err)
	}
}

func (c Cluster) ApplyConfigDir(ns string, configDir string) error {
	return filepath.Walk(configDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		targetPath := c.configDir + string(os.PathSeparator) + path[len(configDir):]
		if info.IsDir() {
			scopes.Framework.Debugf("Making dir: %v", targetPath)
			return os.MkdirAll(targetPath, os.ModePerm)
		}
		scopes.Framework.Debugf("Copying file to: %v", targetPath)
		contents, readerr := ioutil.ReadFile(path)
		if readerr != nil {
			return readerr
		}

		yamlText := string(contents)
		yamlText, err = yml.ApplyNamespace(yamlText, ns)
		if err != nil {
			return err
		}

		_, err = c.cache.Apply(yamlText)
		return err
	})
}

func (c Cluster) DeleteConfigDir(ns string, configDir string) error {
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

		return c.DeleteConfig(ns, string(contents))
	})
}

func (c Cluster) String() string {
	return "nativeCluster"
}

func (c Cluster) Index() resource.ClusterIndex {
	// Multicluster not supported natively.
	return 0
}

// ClusterOrDefault gets the given cluster as a kube Cluster if available. Otherwise
// defaults to the first Cluster in the Environment.
func ClusterOrDefault(c resource.Cluster, e resource.Environment) resource.Cluster {
	if c == nil {
		return e.(*Environment).Cluster
	}
	return c.(Cluster)
}
