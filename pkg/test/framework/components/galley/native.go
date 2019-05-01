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
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/galley/pkg/server"
	"istio.io/istio/pkg/appsignals"
	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/components/environment/native"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/yml"
)

var (
	_ Instance  = &nativeComponent{}
	_ io.Closer = &nativeComponent{}
)

const (
	galleyWorkdir  = "galley-workdir"
	configDir      = "config"
	meshConfigDir  = "mesh-config"
	meshConfigFile = "meshconfig.yaml"
)

// newNative returns the native implementation of galley.Instance.
func newNative(ctx resource.Context, cfg Config) (Instance, error) {

	n := &nativeComponent{
		context:     ctx,
		environment: ctx.Environment().(*native.Environment),
		cfg:         cfg,
	}
	n.id = ctx.TrackResource(n)

	return n, n.Reset()
}

type nativeComponent struct {
	id  resource.ID
	cfg Config

	context     resource.Context
	environment *native.Environment

	client *client

	// Top level home dir for all configuration that is fed to Galley
	homeDir string

	// The folder that Galley reads to local, file-based configuration from
	configDir string

	// The folder that Galley reads the mesh config file from
	meshConfigDir string

	// The file that Galley reads the mesh config file from.
	meshConfigFile string

	server *server.Server

	cache *yml.Cache
}

var _ Instance = &nativeComponent{}

// ID implements resource.Instance
func (c *nativeComponent) ID() resource.ID {
	return c.id
}

// Address of the Galley MCP Server.
func (c *nativeComponent) Address() string {
	return c.client.address
}

// ClearConfig implements Galley.ClearConfig.
func (c *nativeComponent) ClearConfig() (err error) {
	if err := c.cache.Clear(); err != nil {
		return err
	}

	err = c.applyAttributeManifest()
	return
}

// ApplyConfig implements Galley.ApplyConfig.
func (c *nativeComponent) ApplyConfig(ns namespace.Instance, yamlText ...string) error {
	defer appsignals.Notify("galley.native.ApplyConfig", syscall.SIGUSR1)

	var err error
	for _, y := range yamlText {
		y, err = applyNamespace(ns, y)
		if err != nil {
			return err
		}

		if _, err = c.cache.Apply(y); err != nil {
			return nil
		}
	}

	return nil
}

// ApplyConfigOrFail applies the given config yaml file via Galley.
func (c *nativeComponent) ApplyConfigOrFail(t *testing.T, ns namespace.Instance, yamlText ...string) {
	t.Helper()
	err := c.ApplyConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("Galley.ApplyConfigOrFail: %v", err)
	}
}

// DeleteConfig implements Galley.DeleteConfig.
func (c *nativeComponent) DeleteConfig(ns namespace.Instance, yamlText ...string) error {
	defer appsignals.Notify("galley.native.DeleteConfig", syscall.SIGUSR1)

	var err error
	for _, y := range yamlText {
		y, err = applyNamespace(ns, y)
		if err != nil {
			return err
		}

		if err = c.cache.Delete(y); err != nil {
			return err
		}
	}

	return nil
}

// DeleteConfigOrFail implements Galley.DeleteConfigOrFail.
func (c *nativeComponent) DeleteConfigOrFail(t *testing.T, ns namespace.Instance, yamlText ...string) {
	t.Helper()
	err := c.DeleteConfig(ns, yamlText...)
	if err != nil {
		t.Fatalf("Galley.DeleteConfigOrFail: %v", err)
	}
}

// ApplyConfigDir implements Galley.ApplyConfigDir.
func (c *nativeComponent) ApplyConfigDir(ns namespace.Instance, sourceDir string) (err error) {
	defer appsignals.Notify("galley.native.ApplyConfigDir", syscall.SIGUSR1)
	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		targetPath := c.configDir + string(os.PathSeparator) + path[len(sourceDir):]
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
		if ns != nil {
			var err error
			yamlText, err = yml.ApplyNamespace(yamlText, ns.Name())
			if err != nil {
				return err
			}
		}

		_, err = c.cache.Apply(yamlText)
		return err
	})
}

// WaitForSnapshot implements Galley.WaitForSnapshot.
func (c *nativeComponent) WaitForSnapshot(collection string, validator SnapshotValidatorFunc) error {
	return c.client.waitForSnapshot(collection, validator)
}

// WaitForSnapshotOrFail implements Galley.WaitForSnapshotOrFail.
func (c *nativeComponent) WaitForSnapshotOrFail(t *testing.T, collection string, validator SnapshotValidatorFunc) {
	t.Helper()
	if err := c.WaitForSnapshot(collection, validator); err != nil {
		t.Fatalf("WaitForSnapshotOrFail: %v", err)
	}
}

// Reset implements Resettable.Reset.
func (c *nativeComponent) Reset() error {
	_ = c.Close()

	var err error
	if c.homeDir, err = c.context.CreateTmpDirectory(galleyWorkdir); err != nil {
		scopes.Framework.Errorf("Error creating config directory for Galley: %v", err)
		return err
	}
	scopes.Framework.Debugf("Galley home dir: %v", c.homeDir)

	c.configDir = path.Join(c.homeDir, configDir)
	if err = os.MkdirAll(c.configDir, os.ModePerm); err != nil {
		return err
	}
	scopes.Framework.Debugf("Galley config dir: %v", c.configDir)

	c.cache = yml.NewCache(c.configDir)

	c.meshConfigDir = path.Join(c.homeDir, meshConfigDir)
	if err = os.MkdirAll(c.meshConfigDir, os.ModePerm); err != nil {
		return err
	}
	scopes.Framework.Debugf("Galley mesh config dir: %v", c.meshConfigDir)

	scopes.Framework.Debugf("Galley writing mesh config:\n---\n%s\n---\n", c.cfg.MeshConfig)

	c.meshConfigFile = path.Join(c.meshConfigDir, meshConfigFile)
	if err = ioutil.WriteFile(c.meshConfigFile, []byte(c.cfg.MeshConfig), os.ModePerm); err != nil {
		return err
	}

	if err = c.applyAttributeManifest(); err != nil {
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

	if c.cfg.SinkAddress != "" {
		a.SinkAddress = c.cfg.SinkAddress
		a.SinkAuthMode = "NONE"
	}

	s, err := server.New(a)
	if err != nil {
		scopes.Framework.Errorf("Error starting Galley: %v", err)
		return err
	}

	c.server = s

	go s.Run()

	// TODO: This is due to Galley start-up being racy. We should go back to the "Start" based model where
	// return from s.Start() guarantees that all the setup is complete.
	time.Sleep(time.Second)

	c.client = &client{
		address: fmt.Sprintf("tcp://%s", s.Address().String()),
	}

	if err = c.client.waitForStartup(); err != nil {
		return err
	}

	return nil
}

// Close implements io.Closer.
func (c *nativeComponent) Close() (err error) {
	if c.client != nil {
		scopes.Framework.Debugf("%s closing client", c.id)
		err = c.client.Close()
		c.client = nil
	}
	if c.server != nil {
		scopes.Framework.Debugf("%s closing server", c.id)
		err = multierror.Append(c.server.Close()).ErrorOrNil()
		if err != nil {
			scopes.Framework.Infof("Error while Galley server close during reset: %v", err)
		}
		c.server = nil
	}

	scopes.Framework.Debugf("%s close complete (err:%v)", c.id, err)
	return
}

func (c *nativeComponent) applyAttributeManifest() error {
	helmExtractDir, err := c.context.CreateTmpDirectory("helm-mixer-attribute-extract")
	if err != nil {
		return err
	}

	m, err := deployment.ExtractAttributeManifest(helmExtractDir)
	if err != nil {
		return err
	}

	return c.ApplyConfig(nil, m)
}

func applyNamespace(ns namespace.Instance, yamlText string) (out string, err error) {
	out = yamlText
	if ns != nil {
		out, err = yml.ApplyNamespace(yamlText, ns.Name())
	}
	return
}
