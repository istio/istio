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

package kube

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

var _ resource.Cluster = Cluster{}

// Cluster for a Kubernetes cluster. Provides access via a kube.Accessor.
type Cluster struct {
	kube.Accessor
	filename    string
	networkName string
	index       resource.ClusterIndex
}

func (c Cluster) ApplyConfig(ns string, yamlText ...string) error {
	for _, y := range yamlText {
		_, err := c.ApplyContents(ns, y)
		if err != nil {
			return fmt.Errorf("apply: %v", err)
		}
		scopes.Framework.Debugf("Applied config: ns: %s\n%s\n", ns, y)
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
	for _, y := range yamlText {
		err := c.DeleteContents(ns, y)
		if err != nil {
			return fmt.Errorf("apply: %v", err)
		}
		scopes.Framework.Debugf("Deleted config: ns: %s\n%s\n", ns, y)
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
		if info.IsDir() {
			return nil
		}

		scopes.Framework.Debugf("Reading config file to: %v", path)
		contents, readerr := ioutil.ReadFile(path)
		if readerr != nil {
			return readerr
		}

		return c.ApplyConfig(ns, string(contents))
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
	buf := &bytes.Buffer{}

	_, _ = fmt.Fprintf(buf, "Index:               %d\n", c.index)
	_, _ = fmt.Fprintf(buf, "Filename:            %s\n", c.filename)

	return buf.String()
}

// Filename of the kubeconfig file for this cluster.
func (c Cluster) Filename() string {
	return c.filename
}

// NetworkName the cluster is on
func (c Cluster) NetworkName() string {
	return c.networkName
}

// Name provides the name this cluster used by Istio.
func (c Cluster) Name() string {
	return fmt.Sprintf("cluster-%d", c.index)
}

// Index of this cluster within the Environment.
func (c Cluster) Index() resource.ClusterIndex {
	return c.index
}

// ClusterOrDefault gets the given cluster as a kube Cluster if available. Otherwise
// defaults to the first Cluster in the Environment.
func ClusterOrDefault(c resource.Cluster, e resource.Environment) Cluster {
	if c == nil {
		return e.(*Environment).KubeClusters[0]
	}
	return c.(Cluster)
}
