//  Copyright 2018 Istio Authors
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

package galley

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

func newKube(ctx resource.Context, cfg Config) Instance {

	n := &kubeComponent{
		context:     ctx,
		environment: ctx.Environment().(*kube.Environment),
		cfg:         cfg,
	}
	n.id = ctx.TrackResource(n)

	return n
}

type kubeComponent struct {
	id  resource.ID
	cfg Config

	context     resource.Context
	environment *kube.Environment

	client *client
}

var _ Instance = &kubeComponent{}

// ID implements resource.Instance
func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Address of the Galley MCP Server.
func (c *kubeComponent) Address() string {
	return c.client.address
}

// ClearConfig implements Galley.ClearConfig.
func (c *kubeComponent) ClearConfig() (err error) {
	panic("NYI: ClearConfig")
	// TODO
	//infos, err := ioutil.ReadDir(c.configDir)
	//if err != nil {
	//	return err
	//}
	//for _, i := range infos {
	//	err := os.Remove(path.Join(c.configDir, i.Name()))
	//	if err != nil {
	//		return err
	//	}
	//}
	//
	//err = c.applyAttributeManifest()
}

// ApplyConfig implements Galley.ApplyConfig.
func (c *kubeComponent) ApplyConfig(ns namespace.Instance, yamlText ...string) error {
	namespace := ""
	if ns != nil {
		namespace = ns.Name()
	}

	for _, y := range yamlText {
		_, err := c.environment.Accessor.ApplyContents(namespace, y)
		if err != nil {
			return err
		}
		scopes.Framework.Debugf("Applied config: ns: %s\n%s\n", namespace, y)
	}

	return nil
}

// ApplyConfigOrFail applies the given config yaml file via Galley.
func (c *kubeComponent) ApplyConfigOrFail(t *testing.T, ns namespace.Instance, yamlText ...string) {
	t.Helper()
	err := c.ApplyConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("Galley.ApplyConfigOrFail: %v", err)
	}
}

// DeleteConfig implements Galley.DeleteConfig.
func (c *kubeComponent) DeleteConfig(ns namespace.Instance, yamlText ...string) (err error) {
	panic("NYI: DeleteConfig")
}

// DeleteConfigOrFail implements Galley.DeleteConfigOrFail.
func (c *kubeComponent) DeleteConfigOrFail(t *testing.T, ns namespace.Instance, yamlText ...string) {
	t.Helper()
	err := c.DeleteConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("Galley.DeleteConfigOrFail: %v", err)
	}
}

// ApplyConfigDir implements Galley.ApplyConfigDir.
func (c *kubeComponent) ApplyConfigDir(ns namespace.Instance, sourceDir string) (err error) {
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
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

// WaitForSnapshot implements Galley.WaitForSnapshot.
func (c *kubeComponent) WaitForSnapshot(collection string, validator SnapshotValidatorFunc) error {
	panic("NYI: WaitForSnapshot")
	// TODO
	//	return c.client.waitForSnapshot(collection, validator)
}

// Close implements io.Closer.
func (c *kubeComponent) Close() (err error) {
	if c.client != nil {
		scopes.Framework.Debugf("%s closing client", c.id)
		err = c.client.Close()
		c.client = nil
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", c.id, err)
	return
}
