// Copyright 2019 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package deployment

import (
	"istio.io/istio/pkg/test/framework/core"
	"istio.io/istio/pkg/test/util/retry"
)

// Instance of a deployment. Wraps over pkg/test/deployment instances for test framework integration purposes.
type Instance interface {
	core.Resource

	// Name of the deployment, for debugging purposes.
	Name() string

	// Namespace of the deployment, if any.
	Namespace() core.Namespace
}

type Config struct {
	Name string

	// Namespace of deployment. If left empty, default will be used.
	Namespace core.Namespace

	// The yaml contents to deploy.
	Yaml string

	RetryOptions []retry.Option
}

// New returns a new instance of deployment.
func New(ctx core.Context, cfg Config) (i Instance, err error) {
	err = core.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(core.Kube, func() {
		i, err = newKube(ctx, cfg)
	})
	return
}
