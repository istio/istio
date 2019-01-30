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
	"istio.io/istio/pkg/test/framework/api/components"
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
	BookInfoConfig    bookInfoConfig = "bookinfo.yaml"
	BookinfoRatingsv2 bookInfoConfig = "bookinfo-ratings-v2.yaml"
	BookinfoDb        bookInfoConfig = "bookinfo-db.yaml"
)

var _ components.Bookinfo = &kubeComponent{}
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

	return deployBookInfoService(ctx, scope, string(BookInfoConfig))
}

// DeployRatingsV2 deploys ratings v2 service
func (c *kubeComponent) DeployRatingsV2(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	return deployBookInfoService(ctx, scope, string(BookinfoRatingsv2))
}

// DeployMongoDb deploys mongodb service
func (c *kubeComponent) DeployMongoDb(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	return deployBookInfoService(ctx, scope, string(BookinfoDb))
}

func deployBookInfoService(ctx component.Repository, scope lifecycle.Scope, bookinfoYamlFile string) (err error) {
	e, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	scopes.CI.Infof("=== BEGIN: Deploy BookInfoConfig (via Yaml File - %s) ===", bookinfoYamlFile)
	defer func() {
		if err != nil {
			err = fmt.Errorf("BookInfoConfig %s deployment failed: %v", bookinfoYamlFile, err) // nolint:golint
			scopes.CI.Infof("=== FAILED: Deploy BookInfoConfig %s ===", bookinfoYamlFile)
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy BookInfoConfig %s ===", bookinfoYamlFile)
		}
	}()

	yamlFile := path.Join(env.BookInfoKube, bookinfoYamlFile)
	_, err = e.DeployYaml(yamlFile, scope)
	return
}
