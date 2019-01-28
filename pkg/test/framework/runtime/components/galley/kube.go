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

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	testContext "istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
)

var (
	_ components.Galley = &kubeComponent{}
	_ api.Component     = &kubeComponent{}
	_ io.Closer         = &kubeComponent{}
)

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

type kubeComponent struct {
	scope lifecycle.Scope
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Galley
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// ApplyConfig implements Galley.ApplyConfig.
// Do nothing for kube env
func (c *kubeComponent) ApplyConfig(yamlText string) (err error) {
	return
}

// ClearConfig implements Galley.ClearConfig.
// Do nothing for kube env
func (c *kubeComponent) ClearConfig() (err error) {
	return
}

// SetMeshConfig applies the given mesh config yaml file via Galley.
// Do nothing for kube env
func (c *kubeComponent) SetMeshConfig(yamlText string) error {
	return nil
}

// GetGalleyAddress returns the galley mcp server address
// Do nothing for kube env
func (c *kubeComponent) GetGalleyAddress() string {
	return ""

}
func (c *kubeComponent) Start(ctx testContext.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	n := env.NamespaceForScope(scope)
	fetchFn := env.NewSinglePodFetch(n, "istio=galley")
	if err := env.WaitUntilPodsAreReady(fetchFn); err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	return nil
}

// Close stops the kube pilot server.
func (c *kubeComponent) Close() (err error) {
	return nil
}

// WaitForSnapshot implements Galley.WaitForSnapshot.
func (c *kubeComponent) WaitForSnapshot(collection string, snapshot ...map[string]interface{}) error {
	return nil
}
