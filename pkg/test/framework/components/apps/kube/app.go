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

package kube

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"testing"

	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	serviceRegistryKube "istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/application/echo/proto"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/kube"
)

var (
	_ environment.DeployedApp = &app{}
)

type endpoint struct {
	port  *model.Port
	owner *app
}

// Name implements the environment.DeployedAppEndpoint interface
func (e *endpoint) Name() string {
	return e.port.Name
}

// Owner implements the environment.DeployedAppEndpoint interface
func (e *endpoint) Owner() environment.DeployedApp {
	return e.owner
}

// Protocol implements the environment.DeployedAppEndpoint interface
func (e *endpoint) Protocol() model.Protocol {
	return e.port.Protocol
}

func (e *endpoint) makeURL(opts environment.AppCallOptions) *url.URL {
	protocol := string(opts.Protocol)
	switch protocol {
	case environment.AppProtocolHTTP:
	case environment.AppProtocolGRPC:
	case environment.AppProtocolWebSocket:
	default:
		protocol = string(environment.AppProtocolHTTP)
	}

	if opts.Secure {
		protocol += "s"
	}

	host := e.owner.serviceName
	if !opts.UseShortHostname {
		host += "." + e.owner.namespace()
	}
	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(e.port.Port)),
	}
}

type app struct {
	e           *kubernetes.Implementation
	serviceName string
	appName     string
	endpoints   []*endpoint
	forwarder   kube.PortForwarder
	client      *echo.Client
}

var _ environment.DeployedApp = &app{}
var _ environment.DeployedAppEndpoint = &endpoint{}

func newApp(serviceName string, pod kubeApiCore.Pod, e *kubernetes.Implementation) (out environment.DeployedApp, err error) {
	a := &app{
		serviceName: serviceName,
		e:           e,
	}
	defer func() {
		if err != nil {
			_ = a.Close()
		}
	}()

	service, err := e.Accessor.GetService(a.namespace(), serviceName)
	if err != nil {
		return nil, err
	}

	// Get the app name for this service.
	a.appName = service.Labels[appLabel]
	if len(a.appName) == 0 {
		return nil, fmt.Errorf("service does not contain the 'app' label")
	}

	// Extract the endpoints from the service definition.
	a.endpoints = getEndpoints(a, service)

	var grpcPort uint16
	grpcPort, err = a.getGrpcPort()
	if err != nil {
		return nil, err
	}

	// Create a forwarder to the command port of the app.
	a.forwarder, err = e.Accessor.NewPortForwarder(&kube.PodSelectOptions{
		PodNamespace: pod.Namespace,
		PodName:      pod.Name,
	}, 0, grpcPort)
	if err != nil {
		return nil, err
	}
	if err = a.forwarder.Start(); err != nil {
		return nil, err
	}

	a.client, err = echo.NewClient(a.forwarder.Address())
	out = a
	return
}

func (a *app) Close() (err error) {
	if a.client != nil {
		err = a.client.Close()
	}
	if a.forwarder != nil {
		err = multierror.Append(err, a.forwarder.Close()).ErrorOrNil()
	}
	return
}

func (a *app) getGrpcPort() (uint16, error) {
	commandEndpoints := a.EndpointsForProtocol(model.ProtocolGRPC)
	if len(commandEndpoints) == 0 {
		return 0, fmt.Errorf("unable fo find GRPC command port")
	}
	return uint16(commandEndpoints[0].(*endpoint).port.Port), nil
}

func (a *app) Name() string {
	return a.serviceName
}

func (a *app) namespace() string {
	return a.e.KubeSettings().DependencyNamespace
}

func getEndpoints(owner *app, service *kubeApiCore.Service) []*endpoint {
	out := make([]*endpoint, len(service.Spec.Ports))
	for i, servicePort := range service.Spec.Ports {
		out[i] = &endpoint{
			owner: owner,
			port: &model.Port{
				Name:     servicePort.Name,
				Port:     int(servicePort.Port),
				Protocol: serviceRegistryKube.ConvertProtocol(servicePort.Name, servicePort.Protocol),
			},
		}
	}
	return out
}

// Endpoints implements the environment.DeployedApp interface
func (a *app) Endpoints() []environment.DeployedAppEndpoint {
	out := make([]environment.DeployedAppEndpoint, len(a.endpoints))
	for i, e := range a.endpoints {
		out[i] = e
	}
	return out
}

// EndpointsForProtocol implements the environment.DeployedApp interface
func (a *app) EndpointsForProtocol(protocol model.Protocol) []environment.DeployedAppEndpoint {
	out := make([]environment.DeployedAppEndpoint, 0)
	for _, e := range a.endpoints {
		if e.Protocol() == protocol {
			out = append(out, e)
		}
	}
	return out
}

// Call implements the environment.DeployedApp interface
func (a *app) Call(e environment.DeployedAppEndpoint, opts environment.AppCallOptions) ([]*echo.ParsedResponse, error) {
	dst, ok := e.(*endpoint)
	if !ok {
		return nil, fmt.Errorf("supplied endpoint was not created by this environment")
	}

	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// TODO(nmittler): Use an image with the new echo service and invoke the command port rather than scraping logs.
	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// Forward a request from 'this' service to the destination service.
	dstURL := dst.makeURL(opts)
	dstServiceName := dst.owner.Name()
	resp, err := a.client.ForwardEcho(&proto.ForwardEchoRequest{
		Url:   dstURL.String(),
		Count: int32(opts.Count),
		Headers: []*proto.Header{
			{
				Key:   "Host",
				Value: dstServiceName,
			},
		},
	})
	if err != nil {
		return nil, err
	}

	if len(resp) != 1 {
		return nil, fmt.Errorf("unexpected number of responses: %d", len(resp))
	}
	if !resp[0].IsOK() {
		return nil, fmt.Errorf("unexpected response status code: %s", resp[0].Code)
	}
	if resp[0].Host != dstServiceName {
		return nil, fmt.Errorf("unexpected host: %s", resp[0].Host)
	}
	if resp[0].Port != strconv.Itoa(dst.port.Port) {
		return nil, fmt.Errorf("unexpected port: %s", resp[0].Port)
	}

	return resp, nil
}

func (a *app) CallOrFail(e environment.DeployedAppEndpoint, opts environment.AppCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := a.Call(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
