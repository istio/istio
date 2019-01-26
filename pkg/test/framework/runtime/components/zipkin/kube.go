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

package zipkin

import (
	"fmt"
	"io"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	testContext "istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

const (
	serviceName = "zipkin"
	appName     = "zipkin"
)

var (
	_ components.Zipkin = &kubeComponent{}
	_ api.Component     = &kubeComponent{}
	_ io.Closer         = &kubeComponent{}
)

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

type kubeComponent struct {
	*client

	scope     lifecycle.Scope
	forwarder testKube.PortForwarder
	env       *kube.Environment
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Zipkin
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *kubeComponent) Start(ctx testContext.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}
	c.env = env

	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	// Find the Zipkin pod and service, and start forwarding a local port.
	n := env.NamespaceForScope(c.scope)

	fetchFn := env.Accessor.NewSinglePodFetch(n, fmt.Sprintf("app=%s", appName))
	if err := env.Accessor.WaitUntilPodsAreReady(fetchFn); err != nil {
		return err
	}
	pods, err := fetchFn()
	if err != nil {
		return err
	}
	pod := pods[0]

	svc, err := env.Accessor.GetService(n, serviceName)
	if err != nil {
		return err
	}
	port := uint16(svc.Spec.Ports[0].Port)

	options := &testKube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}
	forwarder, err := env.Accessor.NewPortForwarder(options, 0, port)
	if err != nil {
		return err
	}

	if err := forwarder.Start(); err != nil {
		return err
	}
	c.forwarder = forwarder
	scopes.Framework.Debugf("initialized Zipkin port forwarder: %v", forwarder.Address())

	c.client = &client{
		address: forwarder.Address(),
	}
	return nil
}

// Close implements io.Closer.
func (c *kubeComponent) Close() error {
	return c.forwarder.Close()
}
