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

package istio

import (
	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

// Instance represents a deployed Istio instance
type Instance interface {
	resource.Resource

	Settings() Config
}

// SetupConfigFn is a setup function that specifies the overrides of the configuration to deploy Istio.
type SetupConfigFn func(cfg *Config)

// Setup is a setup function that will deploy Istio on Kubernetes environment
func Setup(i *Instance, cfn SetupConfigFn) resource.SetupFn {
	return func(ctx resource.Context) error {
		switch ctx.Environment().EnvironmentName() {
		case environment.Native:
			scopes.Framework.Debugf("istio.Setup: Skipping deployment of Istio on native")

		case environment.Kube:
			cfg, err := DefaultConfig(ctx)
			if err != nil {
				return err
			}
			if cfn != nil {
				cfn(&cfg)
			}
			ins, err := Deploy(ctx, &cfg)
			if err != nil {
				return err
			}
			if i != nil {
				*i = ins
			}
		}

		return nil
	}
}

// Deploy deploys (or attaches to) an Istio deployment and returns a handle. If cfg is nil, then DefaultConfig is used.
func Deploy(ctx resource.Context, cfg *Config) (Instance, error) {
	if cfg == nil {
		c, err := DefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		cfg = &c
	}

	var err error
	scopes.CI.Info("=== BEGIN: Deploy Istio (via Helm Template) ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Deploy Istio ===")
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Deploy Istio ===")
		}
	}()

	var i Instance
	switch ctx.Environment().EnvironmentName() {
	case environment.Kube:
		i, err = deploy(ctx, ctx.Environment().(*kube.Environment), *cfg)
	default:
		err = resource.UnsupportedEnvironment(ctx.Environment())
	}

	return i, err
}
