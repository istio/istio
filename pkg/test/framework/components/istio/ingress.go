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
	"fmt"
	"io"
	"net"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/http/headers"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo/common/scheme"
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
	defaultIngressIstioNameLabel = "ingressgateway"
	defaultIngressIstioLabel     = "istio=" + defaultIngressIstioNameLabel
	defaultIngressServiceName    = "istio-" + defaultIngressIstioNameLabel

	discoveryPort = 15012
)

var (
	getAddressTimeout = retry.Timeout(3 * time.Minute)
	getAddressDelay   = retry.BackoffDelay(500 * time.Millisecond)

	_ ingress.Instance = &ingressImpl{}
	_ io.Closer        = &ingressImpl{}
)

type ingressConfig struct {
	// Service is the kubernetes Service name for the cluster
	Service types.NamespacedName
	// LabelSelector is the value for the label on the ingress kubernetes objects
	LabelSelector string

	// Cluster to be used in a multicluster environment
	Cluster cluster.Cluster
}

func newIngress(ctx resource.Context, cfg ingressConfig) (i ingress.Instance) {
	if cfg.LabelSelector == "" {
		cfg.LabelSelector = defaultIngressIstioLabel
	}
	c := &ingressImpl{
		service:       cfg.Service,
		labelSelector: cfg.LabelSelector,
		env:           ctx.Environment().(*kube.Environment),
		cluster:       ctx.Clusters().GetOrDefault(cfg.Cluster),
		caller:        common.NewCaller(),
	}
	return c
}

type ingressImpl struct {
	service       types.NamespacedName
	labelSelector string

	env     *kube.Environment
	cluster cluster.Cluster
	caller  *common.Caller
}

func (c *ingressImpl) Close() error {
	return c.caller.Close()
}

// getAddressInner returns the external address for the given port. When we don't have support for LoadBalancer,
// the returned net.Addr will have the externally reachable NodePort address and port.
func (c *ingressImpl) getAddressInner(port int) (string, int, error) {
	attempts := 0
	addr, err := retry.UntilComplete(func() (result any, completed bool, err error) {
		attempts++
		result, completed, err = getRemoteServiceAddress(c.env.Settings(), c.cluster, c.service.Namespace, c.labelSelector, c.service.Name, port)
		if err != nil && attempts > 1 {
			// Log if we fail more than once to avoid test appearing to hang
			// LB provision be slow, so timeout here needs to be long we should give context
			scopes.Framework.Warnf("failed to get address for port %v: %v", port, err)
		}
		return
	}, getAddressTimeout, getAddressDelay)
	if err != nil {
		return "", 0, err
	}

	switch v := addr.(type) {
	case string:
		host, portStr, err := net.SplitHostPort(v)
		if err != nil {
			return "", 0, err
		}
		mappedPort, err := strconv.Atoi(portStr)
		if err != nil {
			return "", 0, err
		}
		return host, mappedPort, nil
	case net.TCPAddr:
		return v.IP.String(), v.Port, nil
	}

	return "", 0, fmt.Errorf("failed to get address for port %v", port)
}

// AddressForPort returns the externally reachable host and port of the component for the given port.
func (c *ingressImpl) AddressForPort(port int) (string, int) {
	host, port, err := c.getAddressInner(port)
	if err != nil {
		scopes.Framework.Error(err)
		return "", 0
	}
	return host, port
}

func (c *ingressImpl) Cluster() cluster.Cluster {
	return c.cluster
}

// HTTPAddress returns the externally reachable HTTP host and port (80) of the component.
func (c *ingressImpl) HTTPAddress() (string, int) {
	return c.AddressForPort(80)
}

// TCPAddress returns the externally reachable TCP host and port (31400) of the component.
func (c *ingressImpl) TCPAddress() (string, int) {
	return c.AddressForPort(31400)
}

// HTTPSAddress returns the externally reachable TCP host and port (443) of the component.
func (c *ingressImpl) HTTPSAddress() (string, int) {
	return c.AddressForPort(443)
}

// DiscoveryAddress returns the externally reachable discovery address (15012) of the component.
func (c *ingressImpl) DiscoveryAddress() net.TCPAddr {
	host, port := c.AddressForPort(discoveryPort)
	ip := net.ParseIP(host)
	if ip.String() == "<nil>" {
		// TODO support hostname based discovery address
		return net.TCPAddr{}
	}
	return net.TCPAddr{IP: ip, Port: port}
}

func (c *ingressImpl) Call(options echo.CallOptions) (echo.CallResult, error) {
	return c.callEcho(options)
}

func (c *ingressImpl) CallOrFail(t test.Failer, options echo.CallOptions) echo.CallResult {
	t.Helper()
	resp, err := c.Call(options)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *ingressImpl) callEcho(opts echo.CallOptions) (echo.CallResult, error) {
	var (
		addr string
		port int
	)
	opts = opts.DeepCopy()

	if opts.Port.ServicePort == 0 {
		s, err := c.schemeFor(opts)
		if err != nil {
			return echo.CallResult{}, err
		}
		opts.Scheme = s

		// Default port based on protocol
		switch s {
		case scheme.HTTP:
			addr, port = c.HTTPAddress()
		case scheme.HTTPS:
			addr, port = c.HTTPSAddress()
		case scheme.TCP:
			addr, port = c.TCPAddress()
		default:
			return echo.CallResult{}, fmt.Errorf("ingress: scheme %v not supported. Options: %v+", s, opts)
		}
	} else {
		addr, port = c.AddressForPort(opts.Port.ServicePort)
	}

	if addr == "" || port == 0 {
		scopes.Framework.Warnf("failed to get host and port for %s/%d", opts.Port.Protocol, opts.Port.ServicePort)
	}

	// Even if they set ServicePort, when load balancer is disabled, we may need to switch to NodePort, so replace it.
	opts.Port.ServicePort = port
	if len(opts.Address) == 0 {
		// Default address based on port
		opts.Address = addr
	}
	if opts.HTTP.Headers == nil {
		opts.HTTP.Headers = map[string][]string{}
	}
	if host := opts.GetHost(); len(host) > 0 {
		opts.HTTP.Headers.Set(headers.Host, host)
	}
	if len(c.cluster.HTTPProxy()) > 0 && !c.cluster.ProxyKubectlOnly() {
		opts.HTTP.HTTPProxy = c.cluster.HTTPProxy()
	}
	return c.caller.CallEcho(c, opts)
}

func (c *ingressImpl) schemeFor(opts echo.CallOptions) (scheme.Instance, error) {
	if opts.Scheme == "" && opts.Port.Protocol == "" {
		return "", fmt.Errorf("must provide either protocol or scheme")
	}

	if opts.Scheme != "" {
		return opts.Scheme, nil
	}

	return opts.Port.Scheme()
}

func (c *ingressImpl) PodID(i int) (string, error) {
	pods, err := c.env.Clusters().Default().PodsForSelector(context.TODO(), c.service.Namespace, c.labelSelector)
	if err != nil {
		return "", fmt.Errorf("unable to get ingressImpl gateway stats: %v", err)
	}
	if i < 0 || i >= len(pods.Items) {
		return "", fmt.Errorf("pod index out of boundary (%d): %d", len(pods.Items), i)
	}
	return pods.Items[i].Name, nil
}

func (c *ingressImpl) Namespace() string {
	return c.service.Namespace
}
