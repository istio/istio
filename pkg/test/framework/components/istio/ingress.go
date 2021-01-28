// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package istio

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/client"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/components/echo"
	"istio.io/istio/pkg/test/framework/components/echo/common"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	defaultIngressIstioLabel  = "ingressgateway"
	defaultIngressServiceName = "istio-" + defaultIngressIstioLabel

	eastWestIngressIstioLabel  = "eastwestgateway"
	eastWestIngressServiceName = "istio-" + eastWestIngressIstioLabel

	proxyContainerName = "istio-proxy"
	proxyAdminPort     = 15000
	discoveryPort      = 15012
)

var (
	getAddressTimeout = retry.Timeout(3 * time.Minute)
	getAddressDelay   = retry.Delay(5 * time.Second)

	_ ingress.Instance = &ingressImpl{}
)

type ingressConfig struct {
	// ServiceName is the kubernetes Service name for the cluster
	ServiceName string
	// Namespace the ingress can be found in
	Namespace string
	// IstioLabel is the value for the "istio" label on the ingress kubernetes objects
	IstioLabel string

	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster
}

func newIngress(ctx resource.Context, cfg ingressConfig) (i ingress.Instance) {
	if cfg.ServiceName == "" {
		cfg.ServiceName = defaultIngressServiceName
	}
	if cfg.IstioLabel == "" {
		cfg.IstioLabel = defaultIngressIstioLabel
	}
	c := &ingressImpl{
		serviceName: cfg.ServiceName,
		istioLabel:  cfg.IstioLabel,
		namespace:   cfg.Namespace,
		env:         ctx.Environment().(*kube.Environment),
		cluster:     ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	return c
}

type ingressImpl struct {
	serviceName string
	istioLabel  string
	namespace   string

	env     *kube.Environment
	cluster cluster.Cluster
}

// getAddressInner returns the external address for the given port. When we don't have support for LoadBalancer,
// the returned net.Addr will have the externally reachable NodePort address and port.
func (c *ingressImpl) getAddressInner(port int) (net.TCPAddr, error) {
	addr, err := retry.Do(func() (result interface{}, completed bool, err error) {
		return getRemoteServiceAddress(c.env.Settings(), c.cluster, c.namespace, c.istioLabel, c.serviceName, port)
	}, getAddressTimeout, getAddressDelay)
	if addr != nil {
		return addr.(net.TCPAddr), err
	}
	return net.TCPAddr{}, err
}

// HTTPAddress returns the externally reachable HTTP address (80) of the component.
func (c *ingressImpl) HTTPAddress() net.TCPAddr {
	address, err := c.getAddressInner(80)
	if err != nil {
		return net.TCPAddr{}
	}
	return address
}

// TCPAddress returns the externally reachable TCP address (31400) of the component.
func (c *ingressImpl) TCPAddress() net.TCPAddr {
	address, err := c.getAddressInner(31400)
	if err != nil {
		return net.TCPAddr{}
	}
	return address
}

// HTTPSAddress returns the externally reachable TCP address (443) of the component.
func (c *ingressImpl) HTTPSAddress() net.TCPAddr {
	address, err := c.getAddressInner(443)
	if err != nil {
		return net.TCPAddr{}
	}
	return address
}

// DiscoveryAddress returns the externally reachable discovery address (15012) of the component.
func (c *ingressImpl) DiscoveryAddress() net.TCPAddr {
	address, err := c.getAddressInner(discoveryPort)
	if err != nil {
		scopes.Framework.Errorf(err)
		return net.TCPAddr{}
	}
	return address
}

func (c *ingressImpl) CallEcho(options echo.CallOptions) (client.ParsedResponses, error) {
	return c.callEcho(options, false)
}

func (c *ingressImpl) CallEchoOrFail(t test.Failer, options echo.CallOptions) client.ParsedResponses {
	t.Helper()
	resp, err := c.CallEcho(options)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *ingressImpl) CallEchoWithRetry(options echo.CallOptions,
	retryOptions ...retry.Option) (client.ParsedResponses, error) {
	return c.callEcho(options, true, retryOptions...)
}

func (c *ingressImpl) CallEchoWithRetryOrFail(t test.Failer, options echo.CallOptions,
	retryOptions ...retry.Option) client.ParsedResponses {
	t.Helper()
	resp, err := c.CallEchoWithRetry(options, retryOptions...)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *ingressImpl) callEcho(options echo.CallOptions, retry bool, retryOptions ...retry.Option) (client.ParsedResponses, error) {
	if options.Port == nil || options.Port.Protocol == "" {
		return nil, fmt.Errorf("must provide protocol")
	}
	if options.Port.ServicePort == 0 {
		// Default port based on protocol
		switch options.Port.Protocol {
		case protocol.HTTP:
			options.Port.ServicePort = c.HTTPAddress().Port
		case protocol.HTTPS:
			options.Port.ServicePort = c.HTTPSAddress().Port
		case protocol.TCP:
			options.Port.ServicePort = c.TCPAddress().Port
		default:
			return nil, fmt.Errorf("protocol %v not supported, provide explicit port", options.Port.Protocol)
		}
	}
	if len(options.Address) == 0 {
		// Default host based on protocol
		switch options.Port.Protocol {
		case protocol.HTTP:
			options.Address = c.HTTPAddress().IP.String()
		case protocol.HTTPS:
			options.Address = c.HTTPSAddress().IP.String()
		case protocol.TCP:
			options.Address = c.TCPAddress().IP.String()
		default:
			return nil, fmt.Errorf("protocol %v not supported, provide explicit port", options.Port.Protocol)
		}
	}
	if options.Headers == nil {
		options.Headers = map[string][]string{}
	}
	if host := options.Headers["Host"]; len(host) == 0 {
		options.Headers["Host"] = []string{options.Address}
	}
	return common.CallEcho(&options, retry, retryOptions...)
}

func (c *ingressImpl) ProxyStats() (map[string]int, error) {
	var stats map[string]int
	statsJSON, err := c.adminRequest("stats?format=json")
	if err != nil {
		return stats, fmt.Errorf("failed to get response from admin port: %v", err)
	}
	return c.unmarshalStats(statsJSON)
}

func (c *ingressImpl) PodID(i int) (string, error) {
	pods, err := c.env.Clusters().Default().PodsForSelector(context.TODO(), c.namespace, "istio=ingressgateway")
	if err != nil {
		return "", fmt.Errorf("unable to get ingressImpl gateway stats: %v", err)
	}
	if i < 0 || i >= len(pods.Items) {
		return "", fmt.Errorf("pod index out of boundary (%d): %d", len(pods.Items), i)
	}
	return pods.Items[i].Name, nil
}

// adminRequest makes a call to admin port at ingress gateway proxy and returns error on request failure.
func (c *ingressImpl) adminRequest(path string) (string, error) {
	pods, err := c.env.Clusters().Default().PodsForSelector(context.TODO(), c.namespace, "istio=ingressgateway")
	if err != nil {
		return "", fmt.Errorf("unable to get ingressImpl gateway stats: %v", err)
	}
	podNs, podName := pods.Items[0].Namespace, pods.Items[0].Name
	// Exec onto the pod and make a curl request to the admin port
	command := fmt.Sprintf("curl http://127.0.0.1:%d/%s", proxyAdminPort, path)
	stdout, stderr, err := c.env.Clusters().Default().PodExec(podName, podNs, proxyContainerName, command)
	return stdout + stderr, err
}

type statEntry struct {
	Name  string      `json:"name"`
	Value json.Number `json:"value"`
}

type stats struct {
	StatList []statEntry `json:"stats"`
}

// unmarshalStats unmarshals Envoy stats from JSON format into a map, where stats name is
// key, and stats value is value.
func (c *ingressImpl) unmarshalStats(statsJSON string) (map[string]int, error) {
	statsMap := make(map[string]int)

	var statsArray stats
	if err := json.Unmarshal([]byte(statsJSON), &statsArray); err != nil {
		return statsMap, fmt.Errorf("unable to unmarshal stats from json: %v", err)
	}

	for _, v := range statsArray.StatList {
		if v.Value == "" {
			continue
		}
		tmp, _ := v.Value.Float64()
		statsMap[v.Name] = int(tmp)
	}
	return statsMap, nil
}
