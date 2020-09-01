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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio/ingress"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	ingressServiceName    = "istio-ingressgateway"
	istioLabel            = "ingressgateway"
	DefaultRequestTimeout = 10 * time.Second

	proxyContainerName = "istio-proxy"
	proxyAdminPort     = 15000
	discoveryPort      = 15012
)

var (
	retryTimeout = retry.Timeout(3 * time.Minute)
	retryDelay   = retry.Delay(5 * time.Second)

	_ ingress.Instance = &ingressImpl{}
)

type ingressConfig struct {
	// Namespace the ingress can be found in
	Namespace string
	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

func newIngress(ctx resource.Context, cfg ingressConfig) (i ingress.Instance) {
	c := &ingressImpl{
		clients:   map[clientKey]*http.Client{},
		namespace: cfg.Namespace,
		env:       ctx.Environment().(*kube.Environment),
		cluster:   ctx.Clusters().GetOrDefault(cfg.Cluster),
	}
	return c
}

type ingressImpl struct {
	namespace string
	env       *kube.Environment
	cluster   resource.Cluster

	mu      sync.Mutex
	clients map[clientKey]*http.Client
}

// getAddressInner returns the ingress gateway address for plain text http requests.
func (c *ingressImpl) getAddressInner(cluster resource.Cluster, ns string, port int) (interface{}, bool, error) {
	// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
	// port.
	if !c.env.Settings().LoadBalancerSupported {
		pods, err := cluster.PodsForSelector(context.TODO(), ns, fmt.Sprintf("istio=%s", istioLabel))
		if err != nil {
			return nil, false, err
		}

		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		scopes.Framework.Debugf("Querying ingressImpl, pods:\n%v\n", names)
		if len(pods.Items) == 0 {
			return nil, false, fmt.Errorf("no ingressImpl pod found")
		}

		scopes.Framework.Debugf("Found pod: \n%v\n", pods.Items[0].Name)
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the ingressImpl node yet")
		}

		svc, err := cluster.CoreV1().Services(ns).Get(context.TODO(), ingressServiceName, v1.GetOptions{})
		if err != nil {
			return nil, false, err
		}

		scopes.Framework.Debugf("Found service for the gateway:\n%v\n", svc)
		if len(svc.Spec.Ports) == 0 {
			return nil, false, fmt.Errorf("no ports found in service: %s/%s", ns, "istio-ingressgateway")
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == int32(port) {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return nil, false, fmt.Errorf("no port %d found in service: %s/%s", port, ns, "istio-ingressgateway")
		}

		return net.TCPAddr{IP: net.ParseIP(ip), Port: int(nodePort)}, true, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := cluster.CoreV1().Services(ns).Get(context.TODO(), ingressServiceName, v1.GetOptions{})
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service ingressImpl is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: port}, true, nil
}

// HTTPAddress returns HTTP address of ingress gateway.
func (c *ingressImpl) HTTPAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.cluster, c.namespace, 80)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

// TCPAddress returns TCP address of ingress gateway.
func (c *ingressImpl) TCPAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.cluster, c.namespace, 31400)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

// HTTPSAddress returns HTTPS IP address and port number of ingress gateway.
func (c *ingressImpl) HTTPSAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.cluster, c.namespace, 443)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

func (c *ingressImpl) DiscoveryAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.cluster, c.namespace, discoveryPort)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

type clientKey struct {
	Address    string
	Host       string
	Timeout    time.Duration
	CallType   ingress.CallType
	PrivateKey string
	CaCert     string
	Cert       string
}

// createClient creates a client which sends HTTP requests or HTTPS requests, depending on
// ingress type. If host is not empty, the client will resolve domain name and verify server
// cert using the host name.
func (c *ingressImpl) createClient(options ingress.CallOptions) (*http.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := clientKeyFromOptions(options)
	if client, ok := c.clients[key]; ok {
		return client, nil
	}
	client := &http.Client{
		Timeout: DefaultRequestTimeout,
	}
	if options.CallType != ingress.PlainText {
		scopes.Framework.Debug("Prepare root cert for client")
		roots := x509.NewCertPool()
		ok := roots.AppendCertsFromPEM([]byte(options.CaCert))
		if !ok {
			return nil, fmt.Errorf("failed to parse root certificate")
		}
		tlsConfig := &tls.Config{
			RootCAs:    roots,
			ServerName: options.Host,
		}
		if options.CallType == ingress.Mtls {
			cer, err := tls.X509KeyPair([]byte(options.Cert), []byte(options.PrivateKey))
			if err != nil {
				return nil, fmt.Errorf("failed to parse private key and server cert")
			}
			tlsConfig.Certificates = []tls.Certificate{cer}
		}
		tr := &http.Transport{
			TLSClientConfig: tlsConfig,
			DialTLSContext: func(ctx context.Context, netw, addr string) (net.Conn, error) {
				if s := strings.Split(addr, ":"); s[0] == options.Host {
					addr = options.Address.String()
				}
				tc, err := tls.DialWithDialer(&net.Dialer{Timeout: DefaultRequestTimeout}, netw, addr, tlsConfig)
				if err != nil {
					scopes.Framework.Errorf("TLS dial fail: %v", err)
					return nil, err
				}
				if err := tc.Handshake(); err != nil {
					scopes.Framework.Errorf("SSL handshake fail: %v", err)
					return nil, err
				}
				return tc, nil
			}}
		client.Transport = tr
	}
	c.clients[key] = client
	return client, nil
}

// clientKeyFromOptions takes the parts of options unique to a client
// to use as the key to prevent creating identical clients.
func clientKeyFromOptions(options ingress.CallOptions) clientKey {
	return clientKey{
		CallType:   options.CallType,
		PrivateKey: options.PrivateKey,
		Cert:       options.Cert,
		CaCert:     options.CaCert,
		Host:       options.Host,
		Address:    options.Address.String(),
	}
}

// createRequest returns a request for client to send, or nil and error if request is failed to generate.
func (c *ingressImpl) createRequest(options ingress.CallOptions) (*http.Request, error) {
	url := "http://" + options.Address.String() + options.Path
	if options.CallType != ingress.PlainText {
		url = "https://" + options.Host + ":" + strconv.Itoa(options.Address.Port) + options.Path
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if options.Host != "" {
		req.Host = options.Host
	}
	if options.Headers != nil {
		req.Header = options.Headers.Clone()
	}

	scopes.Framework.Debugf("Created a request to send %v", req)
	return req, nil
}

func (c *ingressImpl) Call(options ingress.CallOptions) (ingress.CallResponse, error) {
	if err := options.Sanitize(); err != nil {
		scopes.Framework.Fatalf("CallOptions sanitization failure, error %v", err)
	}
	client, err := c.createClient(options)
	if err != nil {
		scopes.Framework.Errorf("failed to create test client, error %v", err)
		return ingress.CallResponse{}, err
	}
	req, err := c.createRequest(options)
	if err != nil {
		scopes.Framework.Errorf("failed to create request, error %v", err)
		return ingress.CallResponse{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return ingress.CallResponse{}, err
	}
	scopes.Framework.Debugf("Received response from %q: %v", req.URL, resp.StatusCode)

	defer func() { _ = resp.Body.Close() }()

	var ba []byte
	ba, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", options.Address.String(), err)
		return ingress.CallResponse{}, err
	}
	contents := string(ba)
	status := resp.StatusCode

	response := ingress.CallResponse{
		Code: status,
		Body: contents,
	}

	return response, nil
}

func (c *ingressImpl) CallOrFail(t test.Failer, options ingress.CallOptions) ingress.CallResponse {
	t.Helper()
	resp, err := c.Call(options)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *ingressImpl) ProxyStats() (map[string]int, error) {
	var stats map[string]int
	statsJSON, err := c.adminRequest("stats?format=json")
	if err != nil {
		return stats, fmt.Errorf("failed to get response from admin port: %v", err)
	}
	return c.unmarshalStats(statsJSON)
}

func (c *ingressImpl) CloseClients() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, cl := range c.clients {
		cl.CloseIdleConnections()
	}
	c.clients = map[clientKey]*http.Client{}
}

func (c *ingressImpl) PodID(i int) (string, error) {
	pods, err := c.env.KubeClusters[0].PodsForSelector(context.TODO(), c.namespace, "istio=ingressgateway")
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
	pods, err := c.env.KubeClusters[0].PodsForSelector(context.TODO(), c.namespace, "istio=ingressgateway")
	if err != nil {
		return "", fmt.Errorf("unable to get ingressImpl gateway stats: %v", err)
	}
	podNs, podName := pods.Items[0].Namespace, pods.Items[0].Name
	// Exec onto the pod and make a curl request to the admin port
	command := fmt.Sprintf("curl http://127.0.0.1:%d/%s", proxyAdminPort, path)
	stdout, stderr, err := c.env.KubeClusters[0].PodExec(podName, podNs, proxyContainerName, command)
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
