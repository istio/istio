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
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/test/application/echo"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/util"
)

const (
	containerName = "app"
	appLabel      = "app"
)

var (
	idRegex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex    = regexp.MustCompile("ServicePort=(.*)")
	codeRegex    = regexp.MustCompile("StatusCode=(.*)")
	hostRegex    = regexp.MustCompile("Host=(.*)")

	_ environment.DeployedApp = &client{}
)

type endpoint struct {
	port  *model.Port
	owner *client
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

type client struct {
	e           *kubernetes.Implementation
	serviceName string
	appName     string
	endpoints   []*endpoint
}

var _ environment.DeployedApp = &client{}
var _ environment.DeployedAppEndpoint = &endpoint{}

func newClient(serviceName string, e *kubernetes.Implementation) (environment.DeployedApp, error) {
	a := &client{
		serviceName: serviceName,
		e:           e,
	}

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
	return a, nil
}

func (a *client) Name() string {
	return a.serviceName
}

func (a *client) namespace() string {
	return a.e.KubeSettings().DependencyNamespace
}

func getEndpoints(owner *client, service *corev1.Service) []*endpoint {
	out := make([]*endpoint, len(service.Spec.Ports))
	for i, servicePort := range service.Spec.Ports {
		out[i] = &endpoint{
			owner: owner,
			port: &model.Port{
				Name:     servicePort.Name,
				Port:     int(servicePort.Port),
				Protocol: kube.ConvertProtocol(servicePort.Name, servicePort.Protocol),
			},
		}
	}
	return out
}

// Endpoints implements the environment.DeployedApp interface
func (a *client) Endpoints() []environment.DeployedAppEndpoint {
	out := make([]environment.DeployedAppEndpoint, len(a.endpoints))
	for i, e := range a.endpoints {
		out[i] = e
	}
	return out
}

// EndpointsForProtocol implements the environment.DeployedApp interface
func (a *client) EndpointsForProtocol(protocol model.Protocol) []environment.DeployedAppEndpoint {
	out := make([]environment.DeployedAppEndpoint, 0)
	for _, e := range a.endpoints {
		if e.Protocol() == protocol {
			out = append(out, e)
		}
	}
	return out
}

// Call implements the environment.DeployedApp interface
func (a *client) Call(e environment.DeployedAppEndpoint, opts environment.AppCallOptions) ([]*echo.ParsedResponse, error) {
	ep, ok := e.(*endpoint)
	if !ok {
		return nil, fmt.Errorf("supplied endpoint was not created by this environment")
	}

	// Normalize the count.
	if opts.Count <= 0 {
		opts.Count = 1
	}

	// TODO(nmittler): Use an image with the new echo service and invoke the command port rather than scraping logs.

	pods, err := a.e.Accessor.GetPods(a.namespace(), fmt.Sprintf("app=%s", a.appName))
	if err != nil {
		return nil, err
	}
	podName := pods[0].Name

	// Exec onto the pod and run the client application to make the request to the target service.
	u := ep.makeURL(opts)
	extra := toExtra(opts.Headers)

	responses := make([]*echo.ParsedResponse, opts.Count)

	cmd := fmt.Sprintf("client -url %s -count %d %s", u.String(), opts.Count, extra)
	_, err = util.Retry(20*time.Second, 1*time.Second, func() (interface{}, bool, error) {
		res, e := a.e.Accessor.Exec(a.namespace(), podName, containerName, cmd)
		if e != nil {
			return nil, false, e
		}

		// Parse the individual elements of the responses.
		ids := idRegex.FindAllStringSubmatch(res, -1)
		versions := versionRegex.FindAllStringSubmatch(res, -1)
		ports := portRegex.FindAllStringSubmatch(res, -1)
		codes := codeRegex.FindAllStringSubmatch(res, -1)
		hosts := hostRegex.FindAllStringSubmatch(res, -1)

		// Verify the response lengths.
		if len(ids) != opts.Count {
			return nil, false, fmt.Errorf("incorrect request count. Expected %d, got %d", opts.Count, len(ids))
		}

		for i := 0; i < opts.Count; i++ {
			response := echo.ParsedResponse{}
			responses[i] = &response

			// TODO(nmittler): We're storing the entire logs for each body ... not strictly correct.
			response.Body = res

			response.ID = ids[i][1]

			if i < len(versions) {
				response.Version = versions[i][1]
			}

			if i < len(ports) {
				response.Port = ports[i][1]
			}

			if i < len(codes) {
				response.Code = codes[i][1]
			}

			if i < len(hosts) {
				response.Host = hosts[i][1]
			}
		}

		return nil, true, nil
	})
	if err != nil {
		return nil, err
	}

	return responses, nil
}

func (a *client) CallOrFail(e environment.DeployedAppEndpoint, opts environment.AppCallOptions, t testing.TB) []*echo.ParsedResponse {
	r, err := a.Call(e, opts)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func toExtra(headers http.Header) string {
	out := ""
	for key, values := range headers {
		for _, value := range values {
			out += fmt.Sprintf("-key %s -value %s", key, value)
		}
	}
	return out
}
