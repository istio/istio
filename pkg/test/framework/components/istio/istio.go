// Copyright Istio Authors
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
	"istio.io/istio/pkg/test"
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

// SetupContextFn is a setup function that uses Context for configuration.
type SetupContextFn func(ctx resource.Context) error

// Get returns the Istio component from the context. If there is none an error is returned.
func Get(ctx resource.Context) (Instance, error) {
	var i Instance
	if err := ctx.GetResource(&i); err != nil {
		return nil, err
	}
	return i, nil
}

// GetOrFail returns the Istio component from the context. If there is none the test is failed.
func GetOrFail(f test.Failer, ctx resource.Context) Instance {
	i, err := Get(ctx)
	if err != nil {
		f.Fatal(err)
	}
	return i
}

// Setup is a setup function that will deploy Istio on Kubernetes environment
func Setup(i *Instance, cfn SetupConfigFn, ctxFns ...SetupContextFn) resource.SetupFn {
	return func(ctx resource.Context) error {
		cfg, err := DefaultConfig(ctx)
		if err != nil {
			return err
		}
		if cfn != nil {
			cfn(&cfg)
		}
		for _, ctxFn := range ctxFns {
			if ctxFn != nil {
				err := ctxFn(ctx)
				if err != nil {
					scopes.Framework.Infof("=== FAILED: context setup function [err=%v] ===", err)
					return err
				}
				scopes.Framework.Info("=== SUCCESS: context setup function ===")
			}
		}
		ins, err := Deploy(ctx, &cfg)
		if err != nil {
			return err
		}
		if i != nil {
			*i = ins
		}

		return nil
	}
}

// Deploy deploys (or attaches to) an Istio deployment and returns a handle. If cfg is nil, then DefaultConfig is used.
func Deploy(ctx resource.Context, cfg *Config) (i Instance, err error) {
	if cfg == nil {
		c, err := DefaultConfig(ctx)
		if err != nil {
			return nil, err
		}
		cfg = &c
	}

	scopes.Framework.Infof("=== BEGIN: Deploy Istio [Suite=%s] ===", ctx.Settings().TestID)
	defer func() {
		if err != nil {
			scopes.Framework.Infof("=== FAILED: Deploy Istio [Suite=%s] ===", ctx.Settings().TestID)
		} else {
			scopes.Framework.Infof("=== SUCCEEDED: Deploy Istio [Suite=%s]===", ctx.Settings().TestID)
		}
	}()

	i, err = deploy(ctx, ctx.Environment().(*kube.Environment), *cfg)
	return
}
