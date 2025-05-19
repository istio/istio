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
	"net/netip"
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
	testKube "istio.io/istio/pkg/test/kube"
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

// getAddressesInner returns the external addresses for the given port. When we don't have support for LoadBalancer,
// the returned list will contain will have the externally reachable NodePort address and port.
func (c *ingressImpl) getAddressesInner(port int) ([]string, []int, error) {
	attempts := 0
	rawAddrs, err := retry.UntilComplete(func() (rawAddrs any, completed bool, err error) {
		attempts++
		rawAddrs, completed, err = getRemoteServiceAddresses(c.env.Settings(), c.cluster, c.service.Namespace, c.labelSelector, c.service.Name, port)
		if err != nil && attempts > 1 {
			// Log if we fail more than once to avoid test appearing to hang
			// LB provision be slow, so timeout here needs to be long we should give context
			scopes.Framework.Warnf("failed to get address for port %v: %v", port, err)
		}

		// Check and wait for resolvable ingress DNS name. Skip if IP.
		if err == nil && completed {
			hostPorts, ok := rawAddrs.([]any)
			if !ok || len(hostPorts) == 0 {
				return rawAddrs, completed, err
			}
			v, ok := hostPorts[0].(string)
			if !ok {
				return rawAddrs, completed, err
			}
			host, _, splitErr := net.SplitHostPort(v)
			if splitErr != nil || net.ParseIP(host) != nil {
				// If address is malformed or is already an IP, skip DNS resolution
				return rawAddrs, completed, err
			}
			if _, lookupErr := net.LookupHost(host); lookupErr != nil {
				scopes.Framework.Infof("waiting for DNS to resolve for host %q", host)
				return nil, false, fmt.Errorf("the DNS for %q not ready: %v", host, lookupErr)
			}
		}
		return
	}, getAddressTimeout, getAddressDelay)
	if err != nil {
		return nil, nil, err
	}
	hostPorts, ok := rawAddrs.([]any)
	if !ok {
		return nil, nil, fmt.Errorf("unexpected address format: %T", rawAddrs)
	}

	var addrs []string
	var ports []int
	for _, addr := range hostPorts {
		switch v := addr.(type) {
		case string:
			host, portStr, err := net.SplitHostPort(v)
			if err != nil {
				return nil, nil, err
			}
			mappedPort, err := strconv.Atoi(portStr)
			if err != nil {
				return nil, nil, err
			}
			addrs = append(addrs, host)
			ports = append(ports, mappedPort)
		case netip.AddrPort:
			addrs = append(addrs, v.Addr().String())
			ports = append(ports, int(v.Port()))
		}
	}
	if len(addrs) > 0 {
		return addrs, ports, nil
	}

	return nil, nil, fmt.Errorf("failed to resolve any address for port %d after %d attempts", port, attempts)
}

// AddressForPort returns the externally reachable host and port of the component for the given port.
func (c *ingressImpl) AddressesForPort(port int) ([]string, []int) {
	addrs, ports, err := c.getAddressesInner(port)
	if err != nil {
		scopes.Framework.Error(err)
		return nil, nil
	}
	return addrs, ports
}

func (c *ingressImpl) Cluster() cluster.Cluster {
	return c.cluster
}

// HTTPAddresses returns the externally reachable HTTP hosts and port (80) of the component.
func (c *ingressImpl) HTTPAddresses() ([]string, []int) {
	return c.AddressesForPort(80)
}

// TCPAddresses returns the externally reachable TCP hosts and port (31400) of the component.
func (c *ingressImpl) TCPAddresses() ([]string, []int) {
	return c.AddressesForPort(31400)
}

// HTTPSAddresses returns the externally reachable TCP hosts and port (443) of the component.
func (c *ingressImpl) HTTPSAddresses() ([]string, []int) {
	return c.AddressesForPort(443)
}

// DiscoveryAddresses returns the externally reachable discovery addresses (15012) of the component.
func (c *ingressImpl) DiscoveryAddresses() []netip.AddrPort {
	hosts, ports := c.AddressesForPort(discoveryPort)
	var addrs []netip.AddrPort
	if hosts == nil {
		return []netip.AddrPort{{}}
	}
	for i, host := range hosts {
		ip, err := netip.ParseAddr(host)
		if err != nil {
			return []netip.AddrPort{}
		}
		addrs = append(addrs, netip.AddrPortFrom(ip, uint16(ports[i])))
	}

	return addrs
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
	var addrs []string
	var ports []int
	if opts.Port.ServicePort == 0 {
		s, err := c.schemeFor(opts)
		if err != nil {
			return echo.CallResult{}, err
		}
		opts.Scheme = s

		// Default port based on protocol
		switch s {
		case scheme.HTTP:
			addrs, ports = c.HTTPAddresses()
		case scheme.HTTPS:
			addrs, ports = c.HTTPSAddresses()
		case scheme.TCP:
			addrs, ports = c.TCPAddresses()
		default:
			return echo.CallResult{}, fmt.Errorf("ingress: scheme %v not supported. Options: %v+", s, opts)
		}
	} else {
		addrs, ports = c.AddressesForPort(opts.Port.ServicePort)
	}
	if addrs == nil || ports == nil {
		scopes.Framework.Warnf("failed to get host and port for %s/%d", opts.Port.Protocol, opts.Port.ServicePort)
	}
	addr = addrs[0]
	port = ports[0]

	// When the Ingress is a domain name (in public cloud), it might take a bit of time to make it reachable.
	_, err := testKube.WaitUntilReachableIngress(addr)
	if err != nil {
		return echo.CallResult{}, fmt.Errorf("unable to get reachable ingress. Error: %v", err)
	}

	// Even if they set ServicePort, when load balancer is disabled, we may need to switch to NodePort, so replace it.
	opts.Port.ServicePort = port
	if opts.HTTP.Headers == nil {
		opts.HTTP.Headers = map[string][]string{}
	}
	if host := opts.GetHost(); len(host) > 0 {
		opts.HTTP.Headers.Set(headers.Host, host)
	}
	// Default address based on port
	opts.Address = addr
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

func (c *ingressImpl) ServiceName() string {
	return c.service.Name
}

func (c *ingressImpl) Namespace() string {
	return c.service.Namespace
}
