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

package kubernetes

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/scheme"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/tests/util"
)

const (
	containerName = "app"
	appLabel      = "app"
)

var (
	nilResult    = environment.AppCallResult{}
	idRegex      = regexp.MustCompile("(?i)X-Request-Id=(.*)")
	versionRegex = regexp.MustCompile("ServiceVersion=(.*)")
	portRegex    = regexp.MustCompile("ServicePort=(.*)")
	codeRegex    = regexp.MustCompile("StatusCode=(.*)")
	hostRegex    = regexp.MustCompile("Host=(.*)")
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

// MakeURL implements the environment.DeployedAppEndpoint interface
func (e *endpoint) MakeURL() *url.URL {
	return e.makeURL(true)
}

// MakeShortURL implements the environment.DeployedAppEndpoint interface
func (e *endpoint) MakeShortURL() *url.URL {
	return e.makeURL(false)
}

func (e *endpoint) makeURL(useFullDomain bool) *url.URL {
	protocol := "http"
	switch e.port.Protocol {
	case model.ProtocolGRPC:
		protocol = "grpc"
	case model.ProtocolHTTPS:
		protocol = "https"
	}
	host := e.owner.serviceName
	if useFullDomain {
		host += "." + e.owner.namespace
	}
	return &url.URL{
		Scheme: protocol,
		Host:   net.JoinHostPort(host, strconv.Itoa(e.port.Port)),
	}
}

type app struct {
	serviceName string
	appName     string
	namespace   string
	endpoints   []*endpoint
}

var _ environment.DeployedApp = &app{}

// NewApp creates a new DeployedApp for the given app name/namespace.
func NewApp(serviceName, namespace string) (environment.DeployedApp, error) {
	// Get the yaml config for the service
	yamlBytes, err := util.ShellSilent("kubectl get svc %s -n %s -o yaml", serviceName, namespace)
	if err != nil {
		return nil, err
	}

	// Parse the returned config
	decoder := scheme.Codecs.UniversalDeserializer()
	obj, _, err := decoder.Decode([]byte(yamlBytes), nil, nil)
	if err != nil {
		return nil, err
	}

	// Cast to a service
	service, ok := obj.(*corev1.Service)
	if !ok {
		// This should never happen.
		return nil, fmt.Errorf("returned object was not a service")
	}

	// Get the app name for this service.
	appName := service.Labels[appLabel]
	if len(appName) == 0 {
		return nil, fmt.Errorf("service does not contain the 'app' label")
	}

	a := &app{
		serviceName: service.Name,
		appName:     appName,
		namespace:   namespace,
	}
	a.endpoints = getEndpoints(a, service)
	return a, nil
}

func (a *app) Name() string {
	return a.serviceName
}

func getEndpoints(owner *app, service *corev1.Service) []*endpoint {
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
func (a *app) Call(u *url.URL, count int, headers http.Header) (environment.AppCallResult, error) {
	// Get the pod name of the source app
	pods, err := a.pods()
	if err != nil {
		return nilResult, err
	}
	pod := pods[0]

	// Exec onto the pod and run the client application to make the request to the target service.
	extra := toExtra(headers)
	cmd := fmt.Sprintf("client -url %s -count %d %s", u.String(), count, extra)
	res, err := util.Shell("kubectl exec %s -n %s -c %s -- %s ", pod, a.namespace, containerName, cmd)
	if err != nil {
		return nilResult, err
	}

	// Now convert the raw result to an AppCallDetails
	out := environment.AppCallResult{}
	out.Body = res

	ids := idRegex.FindAllStringSubmatch(res, -1)
	for _, id := range ids {
		out.CallIDs = append(out.CallIDs, id[1])
	}

	// Verify that the expected number of requests were made.
	if len(ids) != count {
		return nilResult, fmt.Errorf("incorrect request count. Expected %d, got %d", count, len(ids))
	}

	versions := versionRegex.FindAllStringSubmatch(res, -1)
	for _, version := range versions {
		out.Version = append(out.Version, version[1])
	}

	ports := portRegex.FindAllStringSubmatch(res, -1)
	for _, port := range ports {
		out.Port = append(out.Port, port[1])
	}

	codes := codeRegex.FindAllStringSubmatch(res, -1)
	for _, code := range codes {
		out.ResponseCode = append(out.ResponseCode, code[1])
	}

	hosts := hostRegex.FindAllStringSubmatch(res, -1)
	for _, host := range hosts {
		out.Host = append(out.Host, host[1])
	}

	return out, nil
}

func (a *app) CallOrFail(u *url.URL, count int, headers http.Header, t testing.TB) environment.AppCallResult {
	r, err := a.Call(u, count, headers)
	if err != nil {
		t.Fatalf("Call to app failed: app='%s', url='%s', err='%v'", a.appName, u, err)
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

func (a *app) requestURL(target *app, port *model.Port, includeDomain bool, path string) string {
	protocol := "http"
	switch port.Protocol {
	case model.ProtocolGRPC:
		protocol = "grpc"
	case model.ProtocolHTTPS:
		protocol = "https"
	}
	host := target.serviceName
	if includeDomain {
		host += "." + target.namespace
	}
	return fmt.Sprintf("%s://%s:%d/%s", protocol, host, port.Port, path)
}

func (a *app) pods() ([]string, error) {
	res, err := util.Shell("kubectl get pods -n %s -l app=%s -o jsonpath='{range .items[*]}{@.metadata.name}{\"\\n\"}'",
		a.namespace, a.appName)
	if err != nil {
		return nil, err
	}

	pods := make([]string, 0)
	for _, line := range strings.Split(res, "\n") {
		pod := strings.TrimSpace(line)
		if len(pod) > 0 {
			pods = append(pods, pod)
		}
	}

	if len(pods) == 0 {
		return nil, fmt.Errorf("unable to find pods for App %s", a.appName)
	}
	return pods, nil
}
