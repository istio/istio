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

package citadel

import (
	"fmt"
	"github.com/hashicorp/go-multierror"

	"go.uber.org/multierr"
	"google.golang.org/grpc"

	istio_mixer_v1 "istio.io/api/mixer/v1"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/kube"
	"log"
)

const (
	citadelService = "istio-citadel"
	grpcPortName     = "grpc-citadel"
)

var KubeComponent = &kubeComponent{}

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.Citadel
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	result, err := c.doInit(e)
	if err != nil {
		return nil, multierror.Prefix(err, "citadel init failed:")
	}
	return result, nil
}

func (c *kubeComponent) doInit(e *kubernetes.Implementation) (interface{}, error) {
	res := &deployedCitadel{
		local: false,
	}
	log.Print("doInit is done.")
	//s := e.KubeSettings()

	//pod, err := e.Accessor.WaitForPodBySelectors(s.IstioSystemNamespace, "istio=citadel")
	//if err != nil {
	//	return nil, err
	//}

	//port, err := getGrpcPort(e)
	//if err != nil {
	//	return nil, err
	//}
	//
	//options := &kube.PodSelectOptions{
	//	PodNamespace: pod.Namespace,
	//	PodName:      pod.Name,
	//}
	//forwarder, err := kube.NewPortForwarder(s.KubeConfig, options, 0, port)
	//if err != nil {
	//	return nil, err
	//}
	//if err := forwarder.Start(); err != nil {
	//	return nil, err
	//}
	//
	//conn, err := grpc.Dial(forwarder.Address(), grpc.WithInsecure())
	//if err != nil {
	//	return nil, err
	//}
	//
	//res.client = istio_mixer_v1.NewMixerClient(conn)
	//res.forwarders = append(res.forwarders, forwarder)

	return res, nil
}

func getGrpcPort(e *kubernetes.Implementation) (uint16, error) {
	svc, err := e.Accessor.GetService(e.KubeSettings().IstioSystemNamespace, citadelService)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", citadelService, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", citadelService)
}

type deployedCitadel struct {
	// Indicates that the component is running in local mode.
	local bool

	conn    *grpc.ClientConn
	client  istio_mixer_v1.MixerClient

	forwarders []kube.PortForwarder
}

func (d *deployedCitadel) CitadelName() string {
	return "Citadel"
}

// Close implements io.Closer.
func (d *deployedCitadel) Close() error {
	var err error
	if d.conn != nil {
		err = multierr.Append(err, d.conn.Close())
		d.conn = nil
	}

	for _, fw := range d.forwarders {
		fw.Close()
	}

	return err
}
