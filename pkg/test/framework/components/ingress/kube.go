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
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceName = "istio-ingressgateway"
	istioLabel  = "ingressgateway"
)

var (
	retryTimeout = retry.Timeout(3 * time.Minute)
	retryDelay   = retry.Delay(5 * time.Second)

	_ Instance = &kubeComponent{}
)

type kubeComponent struct {
	id          resource.ID
	address     string
	gatewayType IgType
	caCert      string
	tlsCert     string
	tlsKey      string
}

// getHTTPAddress returns the ingress gateway address for plain text http requests.
func getHTTPAddress(env *kube.Environment, cfg Config) (interface{}, bool, error) {
	// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
	// port.
	n := cfg.Istio.Settings().IngressNamespace
	if env.Settings().Minikube {
		pods, err := env.GetPods(n, fmt.Sprintf("istio=%s", istioLabel))
		if err != nil {
			return nil, false, err
		}

		scopes.Framework.Debugf("Querying ingress, pods:\n%v\n", pods)
		if len(pods) == 0 {
			return nil, false, errors.New("no ingress pod found")
		}

		scopes.Framework.Debugf("Found pod: \n%v\n", pods[0])
		ip := pods[0].Status.HostIP
		if ip == "" {
			return nil, false, errors.New("no Host IP available on the ingress node yet")
		}

		svc, err := env.Accessor.GetService(n, serviceName)
		if err != nil {
			return nil, false, err
		}

		scopes.Framework.Debugf("Found service for the gateway:\n%v\n", svc)
		if len(svc.Spec.Ports) == 0 {
			return nil, false, fmt.Errorf("no ports found in service: %s/%s", n, "istio-ingressgateway")
		}

		port := svc.Spec.Ports[0].NodePort

		return fmt.Sprintf("http://%s:%d", ip, port), true, nil
	}

	// Otherwise, get the load balancer IP.
	svc, err := env.Accessor.GetService(n, serviceName)
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return fmt.Sprintf("http://%s", ip), true, nil
}

// getHTTPSAddress returns the ingress gateway address for https requests.
func getHTTPSAddress(env *kube.Environment, cfg Config) (interface{}, bool, error) {
	n := cfg.Istio.Settings().IngressNamespace

	// Otherwise, get the load balancer IP.
	svc, err := env.Accessor.GetService(n, serviceName)
	if err != nil {
		return nil, false, err
	}

	if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
		return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Namespace, svc.Name)
	}

	ip := svc.Status.LoadBalancer.Ingress[0].IP
	return ip, true, nil
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{}
	c.id = ctx.TrackResource(c)
	c.gatewayType = cfg.IngressType
	c.caCert = cfg.CaCert
	c.tlsCert = cfg.Cert
	c.tlsKey = cfg.PrivateKey

	env := ctx.Environment().(*kube.Environment)

	address, err := retry.Do(func() (interface{}, bool, error) {

		if cfg.IngressType == PlainText {
			return getHTTPAddress(env, cfg)
		}
		return getHTTPSAddress(env, cfg)
	}, retryTimeout, retryDelay)

	if err != nil {
		return nil, err
	}
	c.address = address.(string)

	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

// Address implements environment.DeployedIngress
func (c *kubeComponent) Address() string {
	return c.address
}

// createClient creates a client which sends HTTP requests or HTTPS requests, depending on
// ingress type. If host is not empty, the client will resolve domain name and verify server
// cert using the host name.
func (c *kubeComponent) createClient(host string) (*http.Client, error) {
	client := &http.Client{
		Timeout: 1 * time.Minute,
	}
	if c.gatewayType != PlainText {
		scopes.Framework.Debug("Prepare root cert for client")
		roots := x509.NewCertPool()
		ok := roots.AppendCertsFromPEM([]byte(c.caCert))
		if !ok {
			return nil, errors.New("failed to parse root certificate")
		}
		tlsConfig := &tls.Config{
			RootCAs:    roots,
			ServerName: host,
		}
		if c.gatewayType == Mtls {
			cer, err := tls.X509KeyPair([]byte(c.tlsCert), []byte(c.tlsKey))
			if err != nil {
				return nil, errors.New("failed to parse private key and server cert")
			}
			tlsConfig.Certificates = []tls.Certificate{cer}
		}
		tr := &http.Transport{
			TLSClientConfig: tlsConfig,
			DialTLS: func(netw, addr string) (net.Conn, error) {
				if addr == host+":443" {
					addr = c.address + ":443"
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
func (c *kubeComponent) createRequest(path, host string) (*http.Request, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := c.address + path
	if c.gatewayType != PlainText {
		url = "https://" + host + ":443" + path
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	if c.gatewayType != PlainText && host != "" {
		req.Host = host
	}

	scopes.Framework.Debugf("Created a request to send %v", req)
	return req, nil
}

func (c *kubeComponent) Call(path, host string) (CallResponse, error) {
	client, err := c.createClient(host)
	if err != nil {
		scopes.Framework.Errorf("failed to create test client, error %v", err)
		return CallResponse{}, err
	}
	req, err := c.createRequest(path, host)
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
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", c.address, err)
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

func (c *kubeComponent) CallOrFail(t test.Failer, path, host string) CallResponse {
	t.Helper()
	resp, err := c.Call(path, host)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}
