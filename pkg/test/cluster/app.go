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

package cluster

import (
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/test"
	"istio.io/istio/tests/util"
)

const (
	containerName = "app"
)

var (
	nilResult    = test.AppCallResult{}
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

// Endpoints implements the test.DeployedAppEndpoint interface
func (e *endpoint) Name() string {
	return e.port.Name
}

// Endpoints implements the test.DeployedAppEndpoint interface
func (e *endpoint) Owner() test.DeployedApp {
	return e.owner
}

// Endpoints implements the test.DeployedAppEndpoint interface
func (e *endpoint) Protocol() model.Protocol {
	return e.port.Protocol
}

// Endpoints implements the test.DeployedAppEndpoint interface
func (e *endpoint) MakeURL(useFullDomain bool) string {
	protocol := "http"
	switch e.port.Protocol {
	case model.ProtocolGRPC:
		protocol = "grpc"
	case model.ProtocolHTTPS:
		protocol = "https"
	}
	host := e.owner.name
	if useFullDomain {
		host += "." + e.owner.namespace
	}
	return fmt.Sprintf("%s://%s:%d", protocol, host, e.port.Port)
}

type app struct {
	name      string
	namespace string
	endpoints []*endpoint
}

// NewApp creates a new app object from the given service config
func NewApp(meta model.ConfigMeta, cfg model.Service) test.DeployedApp {
	a := &app{}
	a.name = meta.Name
	a.namespace = meta.Namespace

	for _, port := range cfg.Ports {
		a.endpoints = append(a.endpoints, &endpoint{
			owner: a,
			port:  port,
		})
	}

	return a
}

// Endpoints implements the test.DeployedApp interface
func (a *app) Endpoints() []test.DeployedAppEndpoint {
	out := make([]test.DeployedAppEndpoint, len(a.endpoints))
	for i, e := range a.endpoints {
		out[i] = e
	}
	return out
}

// EndpointsForProtocol implements the test.DeployedApp interface
func (a *app) EndpointsForProtocol(protocol model.Protocol) []test.DeployedAppEndpoint {
	out := make([]test.DeployedAppEndpoint, 0)
	for _, e := range a.endpoints {
		if e.Protocol() == protocol {
			out = append(out, e)
		}
	}
	return out
}

// Call implements the test.DeployedApp interface
func (a *app) Call(url string, count int, headers http.Header) (test.AppCallResult, error) {
	// Get the pod name of the source app
	pods, err := a.pods()
	if err != nil {
		return nilResult, err
	}
	pod := pods[0]

	// Exec onto the pod and run the client application to make the request to the target service.
	extra := toExtra(headers)
	cmd := fmt.Sprintf("client -url %s -count %d %s", url, count, extra)
	res, err := util.Shell("kubectl exec %s -n %s -c %s -- %s ", pod, a.namespace, containerName, cmd)
	if err != nil {
		return nilResult, err
	}

	// Now convert the raw result to an AppCallDetails
	out := test.AppCallResult{}
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
	host := target.name
	if includeDomain {
		host += "." + target.namespace
	}
	return fmt.Sprintf("%s://%s:%d/%s", protocol, host, port.Port, path)
}

func (a *app) pods() ([]string, error) {
	res, err := util.Shell("kubectl get pods -n %s -l app=%s -o jsonpath='{range .items[*]}{@.metadata.name}{\"\\n\"}'",
		a.namespace, a.name)
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
		return nil, fmt.Errorf("unable to find pods for App %s", a.name)
	}
	return pods, nil
}
