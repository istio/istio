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
	"testing"

	"istio.io/istio/pkg/test/framework2/components/environment/kube"

	"istio.io/istio/pkg/test/framework2/core"

	"istio.io/istio/pkg/test/scopes"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

func newKube(s core.Context, e *kube.Environment, cfg Config) (Instance, error) {

	n := &kubeComponent{
		context:     s,
		environment: e,
		cfg:         cfg,
	}
	n.id = s.TrackResource(n)

	return n, nil
}

type kubeComponent struct {
	id  core.ResourceID
	cfg Config

	context     core.Context
	environment *kube.Environment

	client *client
	//
	//// Top level home dir for all configuration that is fed to Galley
	//homeDir string
	//
	//// The folder that Galley reads to local, file-based configuration from
	//configDir string
	//
	//// The folder that Galley reads the mesh config file from
	//meshConfigDir string
	//
	//// The file that Galley reads the mesh config file from.
	//meshConfigFile string
	//
	//server *server.Server
}

var _ Instance = &kubeComponent{}

// ID implements resource.Instance
func (c *kubeComponent) ID() core.ResourceID {
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
	return
}

// ApplyConfig implements Galley.ApplyConfig.
func (c *kubeComponent) ApplyConfig(ns core.Namespace, yamlText ...string)  error {
	namespace := "default"
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
func (c *kubeComponent) ApplyConfigOrFail(t *testing.T, ns core.Namespace, yamlText ...string) {
	t.Helper()
	err := c.ApplyConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("Galley.ApplyConfigOrFail: %v", err)
	}
}

// ApplyConfigDir implements Galley.ApplyConfigDir.
func (c *kubeComponent) ApplyConfigDir(sourceDir string) (err error) {
	panic("NYI: ApplyConfigDir")
	// TODO
	return
	//return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
	//	targetPath := c.configDir + string(os.PathSeparator) + path[len(sourceDir):]
	//	if info.IsDir() {
	//		scopes.Framework.Debugf("Making dir: %v", targetPath)
	//		return os.MkdirAll(targetPath, os.ModePerm)
	//	}
	//	scopes.Framework.Debugf("Copying file to: %v", targetPath)
	//	contents, readerr := ioutil.ReadFile(path)
	//	if readerr != nil {
	//		return readerr
	//	}
	//	return ioutil.WriteFile(targetPath, contents, os.ModePerm)
	//})
}

// WaitForSnapshot implements Galley.WaitForSnapshot.
func (c *kubeComponent) WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error {
	panic("NYI: ApplyConfigDir")
	// TODO
//	return c.client.waitForSnapshot(collection, snapshot)
}

//
//// Reset implements Resettable.Reset.
//func (c *kubeComponent) Reset() error {
//	_ = c.Close()
//
//	var err error
//	if c.homeDir, err = c.context.CreateTmpDirectory(galleyWorkdir); err != nil {
//		scopes.Framework.Errorf("Error creating config directory for Galley: %v", err)
//		return err
//	}
//	scopes.Framework.Debugf("Galley home dir: %v", c.homeDir)
//
//	c.configDir = path.Join(c.homeDir, configDir)
//	if err = os.MkdirAll(c.configDir, os.ModePerm); err != nil {
//		return err
//	}
//	scopes.Framework.Debugf("Galley config dir: %v", c.configDir)
//
//	c.meshConfigDir = path.Join(c.homeDir, meshConfigDir)
//	if err = os.MkdirAll(c.meshConfigDir, os.ModePerm); err != nil {
//		return err
//	}
//	scopes.Framework.Debugf("Galley mesh config dir: %v", c.meshConfigDir)
//
//	scopes.Framework.Debugf("Galley writing mesh config:\n---\n%s\n---\n", c.cfg.MeshConfig)
//
//	c.meshConfigFile = path.Join(c.meshConfigDir, meshConfigFile)
//	if err = ioutil.WriteFile(c.meshConfigFile, []byte(c.cfg.MeshConfig), os.ModePerm); err != nil {
//		return err
//	}
//
//	if err = c.applyAttributeManifest(); err != nil {
//		return err
//	}
//
//	return c.restart()
//}
//
//func (c *kubeComponent) restart() error {
//	a := server.DefaultArgs()
//	a.Insecure = true
//	a.EnableServer = true
//	a.DisableResourceReadyCheck = true
//	a.ConfigPath = c.configDir
//	a.MeshConfigFile = c.meshConfigFile
//	// To prevent ctrlZ port collision between galley/pilot&mixer
//	a.IntrospectionOptions.Port = 0
//	a.ExcludedResourceKinds = make([]string, 0)
//
//	// Bind to an arbitrary port.
//	a.APIAddress = "tcp://0.0.0.0:0"
//
//	s, err := server.New(a)
//	if err != nil {
//		scopes.Framework.Errorf("Error starting Galley: %v", err)
//		return err
//	}
//
//	c.server = s
//
//	go s.Run()
//
//	c.client = &client{
//		address: fmt.Sprintf("tcp://%s", s.Address().String()),
//	}
//
//	if err = c.client.waitForStartup(); err != nil {
//		return err
//	}
//
//	return nil
//}

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
