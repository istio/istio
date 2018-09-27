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
	"reflect"

	"istio.io/istio/pkg/test/deployment"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
)

var (

	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &component{}

	_ environment.BookInfo = &bookInfo{}
)

type component struct {
}

type bookInfo struct {
	ctx environment.ComponentContext
	env *kubernetes.Implementation
}

// ID implements implements component.Component.
func (c *component) ID() dependency.Instance {
	return dependency.BookInfo
}

// Requires implements implements component.Component.
func (c *component) Requires() []dependency.Instance {
	return []dependency.Instance{}
}

// Init implements implements component.Component.
func (c *component) Init(ctx environment.ComponentContext, _ map[dependency.Instance]interface{}) (interface{}, error) {
	env, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %v", reflect.TypeOf(ctx.Environment()))
	}

	return &bookInfo{
		ctx: ctx,
		env: env,
	}, nil
}

func (b *bookInfo) Deploy() error {
	_, err := deployment.NewBookInfo(&deployment.Settings{
		WorkDir:    b.ctx.Settings().WorkDir,
		KubeConfig: b.env.KubeSettings().KubeConfig,
		Namespace:  b.env.KubeSettings().TestNamespace,
		Tag:        b.env.KubeSettings().Tag,
		Hub:        b.env.KubeSettings().Hub,
	},
		deployment.VariantBookInfo,
		b.env.Accessor,
	)

	if err != nil {
		return fmt.Errorf("BookInfo deployment failed: %v", err) // nolint:golint
	}

	return nil
}
