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
	"io"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/fakes/policy"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/native"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	retryDelay = retry.Delay(time.Second)

	_ components.PolicyBackend = &nativeComponent{}
	_ api.Component            = &nativeComponent{}
	_ io.Closer                = &nativeComponent{}
	_ api.Resettable           = &nativeComponent{}
)

// NewNativeComponent factory function for the component
func NewNativeComponent() (api.Component, error) {
	return &nativeComponent{}, nil
}

type nativeComponent struct {
	*client
	namespace string
	scope     lifecycle.Scope
	backend   *policy.Backend
}

func (c *nativeComponent) Descriptor() component.Descriptor {
	return descriptors.PolicyBackend
}

func (c *nativeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *nativeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := native.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	c.namespace = env.Namespace

	c.client = &client{
		env: env,
	}

	scopes.CI.Infof("=== BEGIN: Start local PolicyBackend ===")
	defer func() {
		if err != nil {
			scopes.CI.Infof("=== FAILED: Start local PolicyBackend ===")
			_ = c.Close()
		} else {
			scopes.CI.Infof("=== SUCCEEDED: Start local PolicyBackend ===")
		}
	}()

	c.backend = policy.NewPolicyBackend(0) // auto-allocate port
	if err = c.backend.Start(); err != nil {
		return
	}

	var ctl interface{}
	ctl, err = retry.Do(func() (interface{}, bool, error) {
		c, err := policy.NewController(fmt.Sprintf(":%d", c.backend.Port()))
		if err != nil {
			scopes.Framework.Debugf("error while connecting to the PolicyBackend controller: %v", err)
			return nil, false, err
		}
		return c, true, nil
	}, retryDelay)
	if err != nil {
		return err
	}
	c.client.controller = ctl.(*policy.Controller)
	return nil
}

func (c *nativeComponent) CreateConfigSnippet(name string) string {
	return fmt.Sprintf(
		`apiVersion: "config.istio.io/v1alpha2"
kind: bypass
metadata:
  name: %s
  namespace: %s
spec:
  backend_address: 127.0.0.1:%d
`, name, c.namespace, c.backend.Port())
}

func (c *nativeComponent) Reset() error {
	if c.client != nil {
		return c.client.Reset()
	}
	return nil
}

func (c *nativeComponent) Close() (err error) {
	if c.backend != nil {
		err = multierror.Append(err, c.backend.Close()).ErrorOrNil()
	}
	return
}
