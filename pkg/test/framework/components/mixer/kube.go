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

	"google.golang.org/grpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/mixer/pkg/server"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/kube"
)

var (
	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &kubeComponent{}
)

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.Mixer
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (out interface{}, err error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	dm := &deployedMixer{
		local:       false,
		environment: e,

		// Use the DefaultArgs to get config identity attribute
		args:    server.DefaultArgs(),
		clients: make(map[string]istioMixerV1.MixerClient),
	}
	defer func() {
		if err != nil {
			_ = dm.Close()
		}
	}()

	s := e.KubeSettings()
	for _, serviceType := range []string{telemetryService, policyService} {
		fetchFn := e.Accessor.NewSinglePodFetch(s.IstioSystemNamespace, "istio=mixer", "istio-mixer-type="+serviceType)
		if err := e.Accessor.WaitUntilPodsAreReady(fetchFn); err != nil {
			return nil, err
		}
		pods, err := fetchFn()
		if err != nil {
			return nil, err
		}
		pod := pods[0]

		scopes.Framework.Debugf("completed wait for Mixer pod(%s)", serviceType)

		port, err := getGrpcPort(e, serviceType)
		if err != nil {
			return nil, err
		}
		scopes.Framework.Debugf("extracted grpc port for service: %v", port)

		options := &kube.PodSelectOptions{
			PodNamespace: pod.Namespace,
			PodName:      pod.Name,
		}
		forwarder, err := e.Accessor.NewPortForwarder(options, 0, port)
		if err != nil {
			return nil, err
		}
		if err := forwarder.Start(); err != nil {
			return nil, err
		}
		dm.forwarders = append(dm.forwarders, forwarder)
		scopes.Framework.Debugf("initialized port forwarder: %v", forwarder.Address())

		conn, err := grpc.Dial(forwarder.Address(), grpc.WithInsecure())
		if err != nil {
			return nil, err
		}
		dm.conns = append(dm.conns, conn)
		scopes.Framework.Debug("connected to Mixer pod through port forwarder")

		client := istioMixerV1.NewMixerClient(conn)
		dm.clients[serviceType] = client
	}

	return dm, nil
}

func getGrpcPort(e *kubernetes.Implementation, serviceType string) (uint16, error) {
	svc, err := e.Accessor.GetService(e.KubeSettings().IstioSystemNamespace, "istio-"+serviceType)
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
