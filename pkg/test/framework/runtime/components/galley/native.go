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
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"time"

	multierror "github.com/hashicorp/go-multierror"

	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/scopes"
)

var (
	_ components.Galley = &nativeComponent{}
	_ api.Component     = &nativeComponent{}
	_ io.Closer         = &nativeComponent{}
	_ api.Resettable    = &nativeComponent{}
)

const (
	galleyWorkdir  = "galley-workdir"
	configDir      = "config"
	meshConfigDir  = "mesh-config"
	meshConfigFile = "meshconfig.yaml"
)

// NewNativeComponent factory function for the component
func NewNativeComponent() (api.Component, error) {
	return &nativeComponent{}, nil
}

type nativeComponent struct {
	*client
	ctx   context.Instance
	scope lifecycle.Scope

	// Top level home dir for alll configuration that is fed to Galley
	homeDir string

	// The folder that Galley reads to local, file-based configuration from
	configDir string

	// The folder that Galley reads the mesh config file from
	meshConfigDir string

	// The file that Galley reads the mesh config file from.
	meshConfigFile string

	server *server.Server
}

// Descriptor implements Component.Descriptor.
func (c *nativeComponent) Descriptor() component.Descriptor {
	return descriptors.Galley
}

// Scope implements Component.Scope.
func (c *nativeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// SetMeshConfig applies the given mesh config yaml file via Galley.
func (c *nativeComponent) SetMeshConfig(yamlText string) error {
	if err := ioutil.WriteFile(c.meshConfigFile, []byte(yamlText), os.ModePerm); err != nil {
		return err
	}
	if err := c.Close(); err != nil {
		return err
	}

	return c.restart()
}

// ClearConfig implements Galley.ClearConfig.
func (c *nativeComponent) ClearConfig() (err error) {
	infos, err := ioutil.ReadDir(c.configDir)
	if err != nil {
		return err
	}
	for _, i := range infos {
		err := os.Remove(path.Join(c.configDir, i.Name()))
		if err != nil {
			return err
		}
	}

	return
}

// ApplyConfig implements Galley.ApplyConfig.
func (c *nativeComponent) ApplyConfig(yamlText string) (err error) {
	fn := fmt.Sprintf("cfg-%d.yaml", time.Now().UnixNano())
	fn = path.Join(c.configDir, fn)

	if err = ioutil.WriteFile(fn, []byte(yamlText), os.ModePerm); err != nil {
		return err
	}

	return
}

// WaitForSnapshot implements Galley.WaitForSnapshot.
func (c *nativeComponent) WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error {
	return c.client.waitForSnapshot(collection, snapshot)
}

// Start implements Component.Start.
func (c *nativeComponent) Start(ctx context.Instance, scope lifecycle.Scope) error {
	c.ctx = ctx
	c.scope = scope

	return c.Reset()
}

// Reset implements Resettable.Reset.
func (c *nativeComponent) Reset() error {
	_ = c.Close()

	var err error
	if c.homeDir, err = c.ctx.CreateTmpDirectory(galleyWorkdir); err != nil {
		scopes.Framework.Errorf("Error creating config directory for Galley: %v", err)
		return err
	}
	scopes.Framework.Debugf("Galley home dir: %v", c.homeDir)

	c.configDir = path.Join(c.homeDir, configDir)
	if err = os.MkdirAll(c.configDir, os.ModePerm); err != nil {
		return err
	}
	scopes.Framework.Debugf("Galley config dir: %v", c.configDir)

	c.meshConfigDir = path.Join(c.homeDir, meshConfigDir)
	if err = os.MkdirAll(c.meshConfigDir, os.ModePerm); err != nil {
		return err
	}
	scopes.Framework.Debugf("Galley mesh config dir: %v", c.meshConfigDir)

	c.meshConfigFile = path.Join(c.meshConfigDir, meshConfigFile)
	if err = ioutil.WriteFile(c.meshConfigFile, []byte{}, os.ModePerm); err != nil {
		return err
	}

	return c.restart()
}

func (c *nativeComponent) restart() error {
	a := server.DefaultArgs()
	a.Insecure = true
	a.EnableServer = true
	a.DisableResourceReadyCheck = true
	a.ConfigPath = c.configDir
	a.MeshConfigFile = c.meshConfigFile
	// To prevent ctrlZ port collision between galley/pilot&mixer
	a.IntrospectionOptions.Port = 0
	a.ExcludedResourceKinds = make([]string, 0)

	// Bind to an arbitrary port.
	a.APIAddress = "tcp://0.0.0.0:0"

	s, err := server.New(a)
	if err != nil {
		scopes.Framework.Errorf("Error starting Galley: %v", err)
		return err
	}

	c.server = s

	go s.Run()

	c.client = &client{
		address: fmt.Sprintf("tcp://%s", s.Address().String()),
		ctx:     c.ctx,
	}

	if err = c.client.waitForStartup(); err != nil {
		return err
	}

	return nil
}

// Close implements io.Closer.
func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		err = c.client.Close()
		c.client = nil
	}
	if c.server != nil {
		err := multierror.Append(c.server.Close()).ErrorOrNil()
		if err != nil {
			scopes.Framework.Infof("Error while Galley server close during reset: %v", err)
		}
		c.server = nil
	}
	return
}
