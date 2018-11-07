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

package policybackend

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/util"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/local"
)

var (
	// LocalComponent is a component for the local environment.
	LocalComponent = &localComponent{}
)

type localComponent struct {
}

// ID implements the component.Component interface.
func (c *localComponent) ID() dependency.Instance {
	return dependency.PolicyBackend
}

// Requires implements the component.Component interface.
func (c *localComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *localComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (out interface{}, err error) {
	_, ok := ctx.Environment().(*local.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	be := &policyBackend{
		env:   ctx.Environment(),
		local: true,
	}

	scopes.CI.Infof("=== BEGIN: Start local PolicyBackend ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Start local PolicyBackend ===")
			_ = be.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Start local PolicyBackend ===")
		}
	}()

	be.backend = policy.NewPolicyBackend(0) // auto-allocate port
	if err = be.backend.Start(); err != nil {
		return
	}
	be.prependCloser(be.backend.Close)

	var ctl interface{}
	ctl, err = util.Retry(util.DefaultRetryWait, time.Second, func() (interface{}, bool, error) {
		c, err := policy.NewController(fmt.Sprintf(":%d", be.backend.Port()))
		if err != nil {
			scopes.Framework.Debugf("error while connecting to the PolicyBackend controller: %v", err)
			return nil, false, err
		}
		return c, true, nil
	})
	if err != nil {
		return nil, err
	}
	be.controller = ctl.(*policy.Controller)

	return be, nil
}
