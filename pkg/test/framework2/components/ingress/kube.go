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
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	"istio.io/istio/pkg/test/framework2/core"

	"istio.io/istio/pkg/test/framework2/components/istio"

	"istio.io/istio/pkg/test/framework2/components/environment/kube"

	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceName = "istio-ingressgateway"
	istioLabel  = "ingressgateway"
)

var (
	retryTimeout = retry.Timeout(1 * time.Minute)
	retryDelay   = retry.Delay(5 * time.Second)

	_ Instance = &kubeComponent{}
)

type kubeComponent struct {
	id      core.ResourceID
	address string
}

func newKube(ctx core.Context) (Instance, error) {
	c := &kubeComponent{}
	c.id = ctx.TrackResource(c)

	env := ctx.Environment().(*kube.Environment)

	address, err := retry.Do(func() (interface{}, bool, error) {

		// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
		// port.
		cfg, err := istio.DefaultConfig(ctx)
		if err != nil {
			return nil, false, err
		}

		n := cfg.SystemNamespace
		if cfg.MinikubeIngress {
			pods, err := env.GetPods(n, fmt.Sprintf("istio=%s", istioLabel))
			if err != nil {
				return nil, false, err
			}
			if len(pods) == 0 {
				return nil, false, errors.New("no ingress pod found")
			}
			ip := pods[0].Status.HostIP
			if ip == "" {
				return nil, false, errors.New("no Host IP availale on the ingress node yet")
			}

			svc, err := env.Accessor.GetService(n, serviceName)
			if err != nil {
				return nil, false, err
			}

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
			return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Name, svc.Namespace)
		}

		ip := svc.Status.LoadBalancer.Ingress[0].IP
		return fmt.Sprintf("http://%s", ip), true, nil
	}, retryTimeout, retryDelay)

	if err != nil {
		return nil, err
	}
	c.address = address.(string)

	return c, nil
}

func (c *kubeComponent) ID() core.ResourceID {
	return c.id
}

// Address implements environment.DeployedIngress
func (c *kubeComponent) Address() string {
	return c.address
}

func (c *kubeComponent) Call(path string) (IngressCallResponse, error) {
	client := &http.Client{
		Timeout: 1 * time.Minute,
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := c.address + path

	scopes.Framework.Debugf("Sending call to ingress at: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return IngressCallResponse{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return IngressCallResponse{}, err
	}

	defer func() { _ = resp.Body.Close() }()

	var ba []byte
	ba, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", c.address, err)
		return IngressCallResponse{}, err
	}
	contents := string(ba)
	status := resp.StatusCode

	response := IngressCallResponse{
		Code: status,
		Body: contents,
	}

	// scopes.Framework.Debugf("Received response to ingress call (url: %s): %+v", url, response)

	return response, nil
}
