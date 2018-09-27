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

package ingress

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"reflect"
	"strings"
	"time"

	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
	"istio.io/istio/pkg/test/framework/scopes"
	"istio.io/istio/pkg/test/util"
)

var (

	// KubeComponent is a component for the Kubernetes environment.
	KubeComponent = &component{}

	_ environment.DeployedIngress = &deployedIngress{}
)

type component struct {
}

type deployedIngress struct {
	address string
}

// ID implements implements component.Component.
func (c *component) ID() dependency.Instance {
	return dependency.Ingress
}

// Requires implements implements component.Component.
func (c *component) Requires() []dependency.Instance {
	return []dependency.Instance{}
}

// Init implements implements component.Component.
func (c *component) Init(ctx environment.ComponentContext, _ map[dependency.Instance]interface{}) (interface{}, error) {
	env, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %v", reflect.TypeOf(ctx.Environment()))
	}

	address, err := util.Retry(util.DefaultRetryTimeout, util.DefaultRetryWait, func() (interface{}, bool, error) {

		// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
		// port.
		if env.KubeSettings().NoIngress {
			pods, err := env.Accessor.GetPods(env.KubeSettings().IstioSystemNamespace, "istio=ingressgateway")
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

			svc, err := env.Accessor.GetService(env.KubeSettings().IstioSystemNamespace, "istio-ingressgateway")
			if err != nil {
				return nil, false, err
			}

			if len(svc.Spec.Ports) == 0 {
				return nil, false, fmt.Errorf("no ports found in service: %s/%s",
					env.KubeSettings().IstioSystemNamespace, "istio-ingressgateway")
			}

			port := svc.Spec.Ports[0].NodePort

			return fmt.Sprintf("http://%s:%d", ip, port), true, nil
		}

		// Otherwise, get the load balancer IP.
		svc, err := env.Accessor.GetService(env.KubeSettings().IstioSystemNamespace, "istio-ingressgateway")
		if err != nil {
			return nil, false, err
		}

		if len(svc.Status.LoadBalancer.Ingress) == 0 || svc.Status.LoadBalancer.Ingress[0].IP == "" {
			return nil, false, fmt.Errorf("service ingress is not available yet: %s/%s", svc.Name, svc.Namespace)
		}

		ip := svc.Status.LoadBalancer.Ingress[0].IP
		return fmt.Sprintf("http://%s", ip), true, nil
	})

	if err != nil {
		return nil, err
	}

	return &deployedIngress{
		address: address.(string),
	}, nil
}

// Address implements environment.DeployedIngress
func (i *deployedIngress) Address() string {
	return i.address
}

func (i *deployedIngress) Call(path string) (environment.IngressCallResponse, error) {
	client := &http.Client{
		Timeout: 1 * time.Minute,
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := i.address + path

	scopes.Framework.Debugf("Sending call to ingress at: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return environment.IngressCallResponse{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return environment.IngressCallResponse{}, err
	}

	defer func() { _ = resp.Body.Close() }()

	var ba []byte
	ba, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", i.address, err)
		return environment.IngressCallResponse{}, err
	}
	contents := string(ba)
	status := resp.StatusCode

	response := environment.IngressCallResponse{
		Code: status,
		Body: contents,
	}

	// scopes.Framework.Debugf("Received response to ingress call (url: %s): %+v", url, response)

	return response, nil
}
