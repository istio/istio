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
	"net"

	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
)

var _ Instance = &kubeComponent{}
var _ io.Closer = &kubeComponent{}

type kubeComponent struct {
	id resource.ID

	*client

	env *kube.Environment
}

// NewKubeComponent factory function for the component
func newKube(ctx resource.Context, _ Config) (*kubeComponent, error) {
	c := &kubeComponent{
		env: ctx.Environment().(*kube.Environment),
	}

	c.client = &client{
		local: false,
		env:   c.env,

		// Use the DefaultArgs to get config identity attribute
		args:    server.DefaultArgs(),
		clients: make(map[string]istioMixerV1.MixerClient),
	}

	// TODO: This should be obtained from an Istio deployment.
	cfg, err := istio.DefaultConfig(ctx)
	if err != nil {
		return nil, err
	}
	ns := cfg.SystemNamespace

	for _, serviceType := range []string{telemetryService, policyService} {
		fetchFn := c.env.NewSinglePodFetch(ns, "istio=mixer", "istio-mixer-type="+serviceType)
		pods, err := c.env.WaitUntilPodsAreReady(fetchFn)
		if err != nil {
			return nil, err
		}
		pod := pods[0]

		scopes.Framework.Debugf("completed wait for Mixer pod(%s)", serviceType)

		port, err := getGrpcPort(c.env, ns, serviceType)
		if err != nil {
			return nil, err
		}
		scopes.Framework.Debugf("extracted grpc port for service: %v", port)

		forwarder, err := c.env.NewPortForwarder(pod, 0, port)
		if err != nil {
			return nil, err
		}
		if err = forwarder.Start(); err != nil {
			return nil, err
		}
		c.client.forwarders = append(c.client.forwarders, forwarder)
		scopes.Framework.Debugf("initialized port forwarder: %v", forwarder.Address())

		conn, err := grpc.Dial(forwarder.Address(), grpc.WithInsecure())
		if err != nil {
			return nil, err
		}
		c.client.conns = append(c.client.conns, conn)
		scopes.Framework.Debug("connected to Mixer pod through port forwarder")

		client := istioMixerV1.NewMixerClient(conn)
		c.client.clients[serviceType] = client
	}

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) GetCheckAddress() net.Addr {
	return c.client.server.Addr()
}

func (c *kubeComponent) GetReportAddress() net.Addr {
	return c.client.server.Addr()
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
