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

package mixer

import (
	"fmt"
	"io"
	"testing"
	"time"

	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"
	testKube "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
)

var _ components.Mixer = &kubeComponent{}
var _ api.Component = &kubeComponent{}
var _ io.Closer = &kubeComponent{}

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

type kubeComponent struct {
	*client

	env   *kube.Environment
	scope lifecycle.Scope
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Mixer
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

// TODO(nmittler): Remove this.
func (c *kubeComponent) Configure(t testing.TB, scope lifecycle.Scope, contents string) {
	contents, err := c.env.Evaluate(contents)
	if err != nil {
		c.env.DumpState(t.Name())
		t.Fatalf("Error expanding configuration template: %v", err)
	}

	ns := c.env.NamespaceForScope(scope)
	if _, err := c.env.ApplyContents(ns, contents); err != nil {
		t.Fatal(err)
	}

	// TODO: Implement a mechanism for reliably waiting for the configuration to disseminate in the system.
	// We can use CtrlZ to expose the config state of Mixer and Pilot.
	// See https://github.com/istio/istio/issues/6169 and https://github.com/istio/istio/issues/6170.
	time.Sleep(time.Second * 10)
}

func (c *kubeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	c.env, err = kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	c.client = &client{
		local: false,
		env:   c.env,

		// Use the DefaultArgs to get config identity attribute
		args:    server.DefaultArgs(),
		clients: make(map[string]istioMixerV1.MixerClient),
	}
	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	ns := c.env.NamespaceForScope(scope)

	for _, serviceType := range []string{telemetryService, policyService} {
		fetchFn := c.env.NewSinglePodFetch(ns, "istio=mixer", "istio-mixer-type="+serviceType)
		if err := c.env.WaitUntilPodsAreReady(fetchFn); err != nil {
			return err
		}
		pods, err := fetchFn()
		if err != nil {
			return err
		}
		pod := pods[0]

		scopes.Framework.Debugf("completed wait for Mixer pod(%s)", serviceType)

		port, err := getGrpcPort(c.env, ns, serviceType)
		if err != nil {
			return err
		}
		scopes.Framework.Debugf("extracted grpc port for service: %v", port)

		options := &testKube.PodSelectOptions{
			PodNamespace: pod.Namespace,
			PodName:      pod.Name,
		}
		forwarder, err := c.env.NewPortForwarder(options, 0, port)
		if err != nil {
			return err
		}
		if err := forwarder.Start(); err != nil {
			return err
		}
		c.client.forwarders = append(c.client.forwarders, forwarder)
		scopes.Framework.Debugf("initialized port forwarder: %v", forwarder.Address())

		conn, err := grpc.Dial(forwarder.Address(), grpc.WithInsecure())
		if err != nil {
			return err
		}
		c.client.conns = append(c.client.conns, conn)
		scopes.Framework.Debug("connected to Mixer pod through port forwarder")

		client := istioMixerV1.NewMixerClient(conn)
		c.client.clients[serviceType] = client
	}

	return nil
}

func (c *kubeComponent) Close() error {
	if c.client != nil {
		client := c.client
		c.client = nil
		return client.Close()
	}
	return nil
}

func getGrpcPort(e *kube.Environment, ns, serviceType string) (uint16, error) {
	svc, err := e.Accessor.GetService(ns, "istio-"+serviceType)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", serviceType, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", serviceType)
}
