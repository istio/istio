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
	"net/url"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	errors2 "k8s.io/apimachinery/pkg/api/errors"
	v12 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/resource"

	"istio.io/istio/pilot/pkg/model"
	kube2 "istio.io/istio/pkg/test/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

const (
	serviceName = "istio-ingressgateway"
	istioLabel  = "ingressgateway"
	// Specifies how long we wait before a Secret becomes existent.
	secretWaitTime = 120 * time.Second
	// Name of Secret used by egress
	secretName     = "istio-ingressgateway-certs"
	deploymentName = "istio-ingressgateway"
)

var (
	retryTimeout = retry.Timeout(3 * time.Minute)
	retryDelay   = retry.Delay(5 * time.Second)

	_ Instance = &kubeComponent{}
)

type kubeComponent struct {
	id                   resource.ID
	url                  func(model.Protocol) (*url.URL, error)
	accessor             *kube2.Accessor
	istioSystemNamespace string
}

func newKube(ctx resource.Context, cfg Config) (Instance, error) {
	c := &kubeComponent{}
	c.id = ctx.TrackResource(c)

	env := ctx.Environment().(*kube.Environment)

	c.accessor = env.Accessor
	c.istioSystemNamespace = cfg.Istio.Settings().SystemNamespace
	address, err := retry.Do(func() (interface{}, bool, error) {

		// In Minikube, we don't have the ingress gateway. Instead we do a little bit of trickery to to get the Node
		// port.
		n := cfg.Istio.Settings().SystemNamespace
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

			return func(protocol model.Protocol) (*url.URL, error) {
				for _, p := range svc.Spec.Ports {
					if supportsProtocol(p.Name, protocol) {
						return &url.URL{Scheme: strings.ToLower(string(protocol)), Host: fmt.Sprintf("%s:%d", ip, p.NodePort)}, nil
					}
				}
				return nil, errors.New("no port found")
			}, true, nil
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
		return func(protocol model.Protocol) (*url.URL, error) {
			for _, p := range svc.Spec.Ports {
				if supportsProtocol(p.Name, protocol) {
					return &url.URL{Scheme: strings.ToLower(string(protocol)), Host: fmt.Sprintf("%s:%d", ip, p.Port)}, nil
				}
			}
			return nil, errors.New("no port found")
		}, true, nil
	}, retryTimeout, retryDelay)

	if err != nil {
		return nil, err
	}
	err = c.accessor.WaitForFilesInPod(c.istioSystemNamespace, fmt.Sprintf("istio=%s", istioLabel), []string{"/etc/certs/cert-chain.pem"}, secretWaitTime)
	if err != nil {
		return nil, err
	}

	c.url = address.(func(protocol model.Protocol) (*url.URL, error))
	if cfg.Secret != nil {
		_, err := c.configureSecretAndWaitForExistence(cfg.Secret)
		if err != nil {
			return nil, err
		}
		if cfg.AdditionalSecretMountPoint != "" {
			err = c.addSecretMountPoint(cfg.AdditionalSecretMountPoint)
			if err != nil {
				return nil, err
			}
		}
	}
	return c, nil
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func supportsProtocol(name string, protocol model.Protocol) bool {
	return name == "http" && (protocol == model.ProtocolHTTP || protocol == model.ProtocolHTTP2) ||
		name == "http2" && (protocol == model.ProtocolHTTP || protocol == model.ProtocolHTTP2) ||
		name == "https" && protocol == model.ProtocolHTTPS
}

// URL implements environment.DeployedIngress
func (c *kubeComponent) URL(protocol model.Protocol) (*url.URL, error) {
	return c.url(protocol)
}

func (c *kubeComponent) Call(path string) (CallResponse, error) {
	client := &http.Client{
		Timeout: 1 * time.Minute,
	}

	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url, err := c.url(model.ProtocolHTTP)
	if err != nil {
		return CallResponse{}, err
	}

	url.Path = path

	scopes.Framework.Debugf("Sending call to ingress at: %s", url)

	req, err := http.NewRequest("GET", url.String(), nil)
	if err != nil {
		return CallResponse{}, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return CallResponse{}, err
	}
	scopes.Framework.Debugf("Received response from %q: %v", url, resp.StatusCode)

	defer func() { _ = resp.Body.Close() }()

	var ba []byte
	ba, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		scopes.Framework.Warnf("Unable to connect to read from %s: %v", url, err)
		return CallResponse{}, err
	}
	contents := string(ba)
	status := resp.StatusCode

	response := CallResponse{
		Code: status,
		Body: contents,
	}

	// scopes.Framework.Debugf("Received response to ingress call (url: %s): %+v", url, response)

	return response, nil
}

func (c *kubeComponent) configureSecretAndWaitForExistence(secret *v1.Secret) (*v1.Secret, error) {
	secret.Name = secretName
	secretAPI := c.accessor.GetSecret(c.istioSystemNamespace)
	_, err := secretAPI.Create(secret)
	if err != nil {
		switch t := err.(type) {
		case *errors2.StatusError:
			if t.ErrStatus.Reason == v12.StatusReasonAlreadyExists {
				_, err := secretAPI.Update(secret)
				if err != nil {
					return nil, err
				}
			}
		default:
			return nil, err
		}
	}
	secret, err = c.accessor.WaitForSecret(secretAPI, secretName, secretWaitTime)
	if err != nil {
		return nil, err
	}

	files := make([]string, 0)
	for key := range secret.Data {
		files = append(files, "/etc/istio/ingressgateway-certs/"+key)
	}
	err = c.accessor.WaitForFilesInPod(c.istioSystemNamespace, fmt.Sprintf("istio=%s", istioLabel), files, secretWaitTime)
	if err != nil {
		return nil, err
	}
	return secret, nil
}

func (c *kubeComponent) addSecretMountPoint(path string) error {
	deployment, err := c.accessor.GetDeployment(c.istioSystemNamespace, deploymentName)
	if err != nil {
		return err
	}
	deployment.Spec.Template.Spec.Containers[0].VolumeMounts = append(deployment.Spec.Template.Spec.Containers[0].VolumeMounts,
		v1.VolumeMount{MountPath: path, Name: "ingressgateway-certs", ReadOnly: true})

	_, err = c.accessor.UpdateDeployment(deployment)
	return err
}
