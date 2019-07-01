// Copyright 2019 Istio Authors
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
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"time"

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
}

// getHTTPAddressInner returns the ingress gateway address for plain text http requests.
func getHTTPAddressInner(env *kube.Environment, ns string) (interface{}, bool, error) {
	// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
	// port.
	if env.Settings().Minikube {
		pods, err := env.GetPods(ns, fmt.Sprintf("istio=%s", istioLabel))
		if err != nil {
			return nil, false, err
		}

		scopes.Framework.Debugf("Querying ingress, pods:\n%v\n", pods)
		if len(pods) == 0 {
			return nil, false, fmt.Errorf("no ingress pod found")
		}

		scopes.Framework.Debugf("Found pod: \n%v\n", pods[0])
		ip := pods[0].Status.HostIP
		if ip == "" {
			return nil, false, fmt.Errorf("no Host IP available on the ingress node yet")
		}

		svc, err := env.Accessor.GetService(ns, serviceName)
		if err != nil {
			return nil, false, err
		}

		scopes.Framework.Debugf("Found service for the gateway:\n%v\n", svc)
		if len(svc.Spec.Ports) == 0 {
			return nil, false, fmt.Errorf("no ports found in service: %s/%s", ns, "istio-ingressgateway")
		}

		var nodePort int32
		for _, svcPort := range svc.Spec.Ports {
			if svcPort.Protocol == "TCP" && svcPort.Port == 80 {
				nodePort = svcPort.NodePort
				break
			}
		}
		if nodePort == 0 {
			return nil, false, fmt.Errorf("no port 80 found in service: %s/%s", ns, "istio-ingressgateway")
		}

		return fmt.Sprintf("http://%s:%d", ip, nodePort), true, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := env.Accessor.GetService(ns, serviceName)
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return fmt.Sprintf("http://%s", ip), true, nil
}

// getHTTPSAddressInner returns the ingress gateway address for https requests.
func getHTTPSAddressInner(env *kube.Environment, ns string) (interface{}, bool, error) {
	if env.Settings().Minikube {
		// TODO(JimmyCYJ): Add support into ingress package to fetch address in Minikube environment
		// https://github.com/istio/istio/issues/14180
		return nil, false, fmt.Errorf("fetching HTTPS address in Minikube is not implemented yet")
	}

	svc, err := env.Accessor.GetService(ns, serviceName)
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return ip, true, nil
}

func newKube(ctx resource.Context, cfg Config) Instance {
	c := &kubeComponent{}
	c.id = ctx.TrackResource(c)
	c.namespace = cfg.Istio.Settings().IngressNamespace
	c.env = ctx.Environment().(*kube.Environment)

	return c
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// HTTPAddress returns HTTP address of ingress gateway.
func (c *kubeComponent) HTTPAddress() string {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return getHTTPAddressInner(c.env, c.namespace)
	}, retryTimeout, retryDelay)
	if err != nil {
		return ""
	}
	return address.(string)
}

// HTTPSAddress returns HTTPS address of ingress gateway.
func (c *kubeComponent) HTTPSAddress() string {
	address, err := retry.Do(func() (interface{}, bool, error) {
		return getHTTPSAddressInner(c.env, c.namespace)
	}, retryTimeout, retryDelay)
	if err != nil {
		return ""
	}
	return address.(string)
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
				if addr == options.Host+":443" {
					addr = options.Address + ":443"
				}
				tc, err := tls.Dial(netw, addr, tlsConfig)
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
	url := options.Address + options.Path
	if options.CallType != PlainText {
		url = "https://" + options.Host + ":443" + options.Path
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if options.Host != "" {
		req.Host = options.Host
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
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", options.Address, err)
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
	statsJson, err := c.adminRequest("stats?format=json")
	if err != nil {
		return stats, fmt.Errorf("failed to get response from admin port: %v", err)
	}
	return c.unmarshalStats(statsJson)
}

// adminRequest makes a call to admin port at ingress gateway proxy and returns error on request failure.
func (c *kubeComponent) adminRequest(path string) (string, error) {
	pods, err := c.env.GetPods(c.namespace, "istio=ingressgateway")
	if err != nil {
		return "", fmt.Errorf("unable to get ingress gateway stats: %v", err)
	}
	podNs, podName := pods[0].Namespace, pods[0].Name
	// Exec onto the pod and make a curl request to the admin port
	command := fmt.Sprintf("curl http://127.0.0.1:%d/%s", proxyAdminPort, path)
	return c.env.Accessor.Exec(podNs, podName, proxyContainerName, command)
}

type statEntry struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
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
		statsMap[v.Name] = v.Value
	}
	return statsMap, nil
}
