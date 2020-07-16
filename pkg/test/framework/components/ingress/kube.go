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

package ingress

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
	"time"

	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceName           = "istio-ingressgateway"
	istioLabel            = "ingressgateway"
	DefaultRequestTimeout = 1 * time.Minute

	proxyContainerName = "istio-proxy"
	proxyAdminPort     = 15000
)

var (
	retryTimeout = retry.Timeout(3 * time.Minute)
	retryDelay   = retry.Delay(5 * time.Second)

	_ Instance = &kubeComponent{}
)

type kubeComponent struct {
	id        resource.ID
	namespace string
	env       *kube.Environment
	cluster   resource.Cluster
}

// getHTTPAddressInner returns the ingress gateway address for plain text http requests.
func (c *kubeComponent) getAddressInner(ns string, port int) (interface{}, bool, error) {
	// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
	// port.
	if c.env.Settings().Minikube {
		pods, err := c.cluster.PodsForSelector(context.TODO(), ns, fmt.Sprintf("istio=%s", istioLabel))
		if err != nil {
			return nil, false, err
		}

		names := make([]string, 0, len(pods.Items))
		for _, p := range pods.Items {
			names = append(names, p.Name)
		}
		scopes.Framework.Debugf("Querying ingress, pods:\n%v\n", names)
		if len(pods.Items) == 0 {
			return nil, false, fmt.Errorf("no ingress pod found")
		}

		scopes.Framework.Debugf("Found pod: \n%v\n", pods.Items[0].Name)
		ip := pods.Items[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the ingress node yet")
		}

		svc, err := c.cluster.CoreV1().Services(ns).Get(context.TODO(), serviceName, kubeApiMeta.GetOptions{})
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
	svc, err := c.cluster.CoreV1().Services(ns).Get(context.TODO(), serviceName, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return net.TCPAddr{IP: net.ParseIP(ip), Port: port}, true, nil
}

func newKube(ctx resource.Context, cfg Config) Instance {
	c := &kubeComponent{}
	c.id = ctx.TrackResource(c)
	c.namespace = cfg.Istio.Settings().IngressNamespace
	c.env = ctx.Environment().(*kube.Environment)
	c.cluster = ctx.Clusters().GetOrDefault(cfg.Cluster)

	return c
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// HTTPAddress returns HTTP address of ingress gateway.
func (c *kubeComponent) HTTPAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.namespace, 80)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

// TCPAddress returns TCP address of ingress gateway.
func (c *kubeComponent) TCPAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.namespace, 31400)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

// HTTPSAddress returns HTTPS IP address and port number of ingress gateway.
func (c *kubeComponent) HTTPSAddress() net.TCPAddr {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return c.getAddressInner(c.namespace, 443)
	}, retryTimeout, retryDelay)
	if err != nil {
		return net.TCPAddr{}
	}
	return address.(net.TCPAddr)
}

// createClient creates a client which sends HTTP requests or HTTPS requests, depending on
// ingress type. If host is not empty, the client will resolve domain name and verify server
// cert using the host name.
func (c *kubeComponent) createClient(options CallOptions) (*http.Client, error) {
	client := &http.Client{
		Timeout: options.Timeout,
	}
	if options.CallType != PlainText {
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
		if options.CallType == Mtls {
			cer, err := tls.X509KeyPair([]byte(options.Cert), []byte(options.PrivateKey))
			if err != nil {
				return nil, fmt.Errorf("failed to parse private key and server cert")
			}
			tlsConfig.Certificates = []tls.Certificate{cer}
		}
		tr := &http.Transport{
			TLSClientConfig: tlsConfig,
			DialTLS: func(netw, addr string) (net.Conn, error) {
				if s := strings.Split(addr, ":"); s[0] == options.Host {
					addr = options.Address.String()
				}
				tc, err := tls.DialWithDialer(&net.Dialer{Timeout: options.Timeout}, netw, addr, tlsConfig)
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
	return client, nil
}

// createRequest returns a request for client to send, or nil and error if request is failed to generate.
func (c *kubeComponent) createRequest(options CallOptions) (*http.Request, error) {
	url := "http://" + options.Address.String() + options.Path
	if options.CallType != PlainText {
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

func (c *kubeComponent) Call(options CallOptions) (CallResponse, error) {
	if err := options.sanitize(); err != nil {
		scopes.Framework.Fatalf("CallOptions sanitization failure, error %v", err)
	}
	client, err := c.createClient(options)
	if err != nil {
		scopes.Framework.Errorf("failed to create test client, error %v", err)
		return CallResponse{}, err
	}
	req, err := c.createRequest(options)
	if err != nil {
		scopes.Framework.Errorf("failed to create request, error %v", err)
		return CallResponse{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return CallResponse{}, err
	}
	scopes.Framework.Debugf("Received response from %q: %v", req.URL, resp.StatusCode)

	defer func() { _ = resp.Body.Close() }()

	var ba []byte
	ba, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", options.Address.String(), err)
		return CallResponse{}, err
	}
	contents := string(ba)
	status := resp.StatusCode

	response := CallResponse{
		Code: status,
		Body: contents,
	}

	return response, nil
}

func (c *kubeComponent) CallOrFail(t test.Failer, options CallOptions) CallResponse {
	t.Helper()
	resp, err := c.Call(options)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func (c *kubeComponent) ProxyStats() (map[string]int, error) {
	var stats map[string]int
	statsJSON, err := c.adminRequest("stats?format=json")
	if err != nil {
		return stats, fmt.Errorf("failed to get response from admin port: %v", err)
	}
	return c.unmarshalStats(statsJSON)
}

// adminRequest makes a call to admin port at ingress gateway proxy and returns error on request failure.
func (c *kubeComponent) adminRequest(path string) (string, error) {
	pods, err := c.env.KubeClusters[0].PodsForSelector(context.TODO(), c.namespace, "istio=ingressgateway")
	if err != nil {
		return "", fmt.Errorf("unable to get ingress gateway stats: %v", err)
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
func (c *kubeComponent) unmarshalStats(statsJSON string) (map[string]int, error) {
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
