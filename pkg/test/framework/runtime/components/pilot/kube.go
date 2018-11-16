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

package pilot

import (
	"fmt"
	"io"
	"net"

	"github.com/hashicorp/go-multierror"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	testContext "istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"

	testKube "istio.io/istio/pkg/test/kube"
)

const (
	pilotService = "istio-pilot"
	grpcPortName = "grpc-xds"
)

var (
	_ components.Pilot = &kubeComponent{}
	_ api.Component    = &kubeComponent{}
	_ io.Closer        = &kubeComponent{}
)

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

type kubeComponent struct {
	*client

	forwarder testKube.PortForwarder
	scope     lifecycle.Scope
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Pilot
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

	n := env.NamespaceForScope(scope)
	fetchFn := env.NewSinglePodFetch(n, "istio=pilot")
	if err := env.WaitUntilPodsAreReady(fetchFn); err != nil {
		return err
	}
	pods, err := fetchFn()
	if err != nil {
		return err
	}
	pod := pods[0]

	port, err := getGrpcPort(env, n)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			_ = c.Close()
		}
	}()

	// Start port-forwarding for pilot.
	options := &testKube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}

	c.forwarder, err = env.NewPortForwarder(options, 0, port)
	if err != nil {
		return err
	}
	if err = c.forwarder.Start(); err != nil {
		return err
	}

	var addr *net.TCPAddr
	addr, err = net.ResolveTCPAddr("tcp", c.forwarder.Address())
	if err != nil {
		return err
	}

	c.client, err = newClient(addr)
	if err != nil {
		return err
	}
	return nil
}

// Close stops the kube pilot server.
func (c *kubeComponent) Close() (err error) {
	if c.client != nil {
		err = multierror.Append(err, c.client.Close()).ErrorOrNil()
		c.client = nil
	}

	if c.forwarder != nil {
		err = multierror.Append(err, c.forwarder.Close()).ErrorOrNil()
		c.forwarder = nil
	}
	return
}

func getGrpcPort(e *kube.Environment, ns string) (uint16, error) {
	svc, err := e.Accessor.GetService(ns, pilotService)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", pilotService, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", pilotService)
}
