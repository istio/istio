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
	"path/filepath"
	"testing"

	"github.com/pkg/errors"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	grpcPortName = "grpc-mcp"
)

var (
	_ Instance  = &kubeComponent{}
	_ io.Closer = &kubeComponent{}
)

func newKube(ctx resource.Context, cfg Config) (Instance, error) {

	n := &kubeComponent{
		context:     ctx,
		environment: ctx.Environment().(*kube.Environment),
		cfg:         cfg,
		resources:   make(map[string][]string),
	}
	n.id = ctx.TrackResource(n)

	// TODO: This should be obtained from an Istio deployment.
	c, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	ns := c.SystemNamespace

	fetchFn := n.environment.NewSinglePodFetch(ns, "istio=galley")
	pods, err := n.environment.WaitUntilPodsAreReady(fetchFn)
	if err != nil {
		return nil, err
	}
	pod := pods[0]

	scopes.Framework.Debug("completed wait for Galley pod")

	port, err := getGrpcPort(n.environment, ns)
	if err != nil {
		return nil, err
	}
	scopes.Framework.Debugf("extracted grpc port for service: %v", port)

	if n.forwarder, err = n.environment.NewPortForwarder(pod, 0, port); err != nil {
		return nil, err
	}
	scopes.Framework.Debugf("initialized port forwarder: %v", n.forwarder.Address())

	if err = n.forwarder.Start(); err != nil {
		return nil, err
	}

	n.client = &client{
		address: fmt.Sprintf("tcp://%s", n.forwarder.Address()),
	}

	if err = n.client.waitForStartup(); err != nil {
		return nil, err
	}

	return n, nil
}

type kubeComponent struct {
	id  resource.ID
	cfg Config

	context     resource.Context
	environment *kube.Environment

	client *client

	// Resources, grouped by string
	resources map[string][]string

	forwarder kube2.PortForwarder
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
	for ns, files := range c.resources {
		for _, file := range files {
			err := c.environment.Accessor.Delete(ns, file)
			if err != nil {
				return err
			}
		}
	}

	c.resources = make(map[string][]string)

	return nil
}

// ApplyConfig implements Galley.ApplyConfig.
func (c *kubeComponent) ApplyConfig(ns namespace.Instance, yamlText ...string) error {
	namespace := ""
	if ns != nil {
		namespace = ns.Name()
	}

	for _, y := range yamlText {
		entries, err := c.environment.Accessor.ApplyContents(namespace, y)
		if err != nil {
			return err
		}
		scopes.Framework.Debugf("Applied config: ns: %s\n%s\n", namespace, y)

		files := c.resources[namespace]
		files = append(files, entries...)
		c.resources[namespace] = files
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
	for _, txt := range yamlText {
		err := c.environment.Accessor.DeleteContents(ns.Name(), txt)
		if err != nil {
			return err
		}
	}

	return nil
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
	return c.client.waitForSnapshot(collection, validator)
}

// WaitForSnapshotOrFail implements Galley.WaitForSnapshotOrFail.
func (c *kubeComponent) WaitForSnapshotOrFail(t *testing.T, collection string, validator SnapshotValidatorFunc) {
	t.Helper()
	if err := c.WaitForSnapshot(collection, validator); err != nil {
		t.Fatalf("WaitForSnapshotOrFail: %v", err)
	}
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

func getGrpcPort(e *kube.Environment, ns string) (uint16, error) {
	svc, err := e.Accessor.GetService(ns, "istio-galley")
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service: %v", err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, errors.New("failed to get target port in service")
}
