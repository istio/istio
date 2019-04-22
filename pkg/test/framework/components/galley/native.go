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

	err = c.applyAttributeManifest()
	return
}

// ApplyConfig implements Galley.ApplyConfig.
func (c *nativeComponent) ApplyConfig(ns namespace.Instance, yamlText ...string) error {
	defer appsignals.Notify("galley.native.ApplyConfig", syscall.SIGUSR1)

	// Read information for all files in configDir
	fileInfos, err := readFileInfos(c.configDir)
	if err != nil {
		return err
	}

	// Create a map of the resource descriptors to file resources.
	// We'll use this map for handling updates.
	fileResourceMap := newFileResourceMap(fileInfos)

	for _, y := range yamlText {
		y, err = applyNamespace(ns, y)
		if err != nil {
			return err
		}

		// Parse the file to obtain all resources and their descriptors.
		resources, err := parseResources(y)
		if err != nil {
			return err
		}

		// Look in the existing files for the resources. If found, overwrite the original.
		for i, r := range resources {
			if fileResource := fileResourceMap[r.descriptor]; fileResource != nil {
				// Update the file resource.
				fileResource.update(r.content)

				// Mark this resource as applied so that we don't write it to a new file.
				resources[i] = nil
			}
		}

		// Generate the new content, if any.
		newFileContent := ""
		for _, r := range resources {
			if r != nil {
				newFileContent = yml.JoinString(newFileContent, r.content)
			}
		}

		// If there were new resources, write their content to a new file.
		if len(newFileContent) > 0 {
			fn := fmt.Sprintf("cfg-%d.yaml", time.Now().UnixNano())
			fn = path.Join(c.configDir, fn)

			scopes.Framework.Debugf("Galley.ApplyConfig: %q\n%s\n----\n", fn, newFileContent)
			if err = ioutil.WriteFile(fn, []byte(newFileContent), os.ModePerm); err != nil {
				return err
			}
		}
	}

	// If any existing files were modified, write their contents back to configDir.
	for _, fi := range fileInfos {
		if fi.isUpdated() {
			if err := fi.writeFile(); err != nil {
				return err
			}
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

	// Read information for all files in configDir
	fileInfos, err := readFileInfos(c.configDir)
	if err != nil {
		return err
	}

	// Create a map of the resource descriptors to file resources.
	// We'll use this map for handling updates.
	fileResourceMap := newFileResourceMap(fileInfos)

	for _, y := range yamlText {
		y, err = applyNamespace(ns, y)
		if err != nil {
			return err
		}

		// Parse the file to obtain all resources and their descriptors.
		resources, err := parseResources(y)
		if err != nil {
			return err
		}

		// Look in the existing files for the resources. If found, delete it.
		for _, r := range resources {
			if fileResource := fileResourceMap[r.descriptor]; fileResource != nil {
				// Delete it.
				fileResource.update("")
			}
		}
	}

	// If any existing files were modified, write their contents back to configDir.
	for _, fi := range fileInfos {
		if fi.isUpdated() {
			if err := fi.writeFile(); err != nil {
				return err
			}
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

		return ioutil.WriteFile(targetPath, []byte(yamlText), os.ModePerm)
	})
}

// WaitForSnapshot implements Galley.WaitForSnapshot.
func (c *nativeComponent) WaitForSnapshot(collection string, validator SnapshotValidatorFunc) error {
	return c.client.waitForSnapshot(collection, validator)
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

type resourceInfo struct {
	content    string
	descriptor yml.Descriptor
	updated    bool
}

func (ri *resourceInfo) update(content string) {
	ri.content = content
	ri.updated = true
}

type fileInfo struct {
	name      string
	resources []*resourceInfo
}

func (fi *fileInfo) isUpdated() bool {
	for _, r := range fi.resources {
		if r.updated {
			return true
		}
	}
	return false
}

func (fi *fileInfo) writeFile() error {
	// Merge all the resources into a single document.
	resourceContent := make([]string, 0, len(fi.resources))
	for _, r := range fi.resources {
		if len(r.content) > 0 {
			resourceContent = append(resourceContent, r.content)
		}
	}

	if len(resourceContent) > 0 {
		// Update the file
		newContent := yml.JoinString(resourceContent...)
		return ioutil.WriteFile(fi.name, []byte(newContent), os.ModePerm)
	}

	// The file is now empty - remove it.
	return os.Remove(fi.name)
}

func readFileInfos(configDir string) ([]*fileInfo, error) {
	infos, err := ioutil.ReadDir(configDir)
	if err != nil {
		return nil, err
	}

	fileInfos := make([]*fileInfo, 0)
	for _, i := range infos {
		filename := filepath.Join(configDir, i.Name())
		content, err := ioutil.ReadFile(filename)
		if err != nil {
			return nil, err
		}

		resources, err := parseResources(string(content))
		if err != nil {
			return nil, err
		}

		fileInfos = append(fileInfos, &fileInfo{
			name:      filename,
			resources: resources,
		})
	}

	return fileInfos, nil
}

func newFileResourceMap(fileInfos []*fileInfo) map[yml.Descriptor]*resourceInfo {
	// Create a map of the resource descriptors to file resources.
	// We'll use this map for handling updates.
	fileResourceMap := make(map[yml.Descriptor]*resourceInfo)
	for _, fi := range fileInfos {
		for _, ri := range fi.resources {
			fileResourceMap[ri.descriptor] = ri
		}
	}
	return fileResourceMap
}

func parseResources(yamlText string) ([]*resourceInfo, error) {
	splitContent := yml.SplitString(yamlText)
	resources := make([]*resourceInfo, 0, len(splitContent))
	for _, part := range splitContent {
		if len(part) > 0 {
			descriptor, err := yml.ParseDescriptor(part)
			if err != nil {
				return nil, err
			}

			resources = append(resources, &resourceInfo{
				content:    part,
				descriptor: descriptor,
			})
		}
	}
	return resources, nil
}
