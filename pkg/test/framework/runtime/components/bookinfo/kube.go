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

package bookinfo

import (
	"fmt"
	"path"

	"istio.io/istio/pkg/test/env"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	"istio.io/istio/pkg/test/scopes"
)

type bookInfoConfig string

const (
	// BookInfoConfig uses "bookinfo.yaml"
	BookInfoConfig bookInfoConfig = "bookinfo.yaml"
)

var _ api.Component = &kubeComponent{}

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

type kubeComponent struct {
	scope lifecycle.Scope
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.BookInfo
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// Init implements implements component.Component.
func (c *kubeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	e, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	scopes.CI.Info("=== BEGIN: Deploy BookInfoConfig (via Yaml File) ===")
	defer func() {
		if err != nil {
			err = fmt.Errorf("BookInfoConfig deployment failed: %v", err) // nolint:golint
			scopes.CI.Infof("=== FAILED: Deploy BookInfoConfig ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy BookInfoConfig ===")
		}
	}()

	yamlFile := path.Join(env.BookInfoKube, string(BookInfoConfig))
	_, err = e.DeployYaml(yamlFile, scope)
	return
}
