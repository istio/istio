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

package citadel

import (
	"fmt"

	"github.com/hashicorp/go-multierror"

	"time"

	"k8s.io/api/core/v1"
	mv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
	"istio.io/istio/pkg/test/framework/environments/kubernetes"
)

const (
	citadelService = "istio-citadel"
	grpcPortName   = "grpc-citadel"
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
	// Name of secret created by Citadel
	secretName = "istio.default"
)

var KubeComponent = &kubeComponent{}

type kubeComponent struct {
}

// ID implements the component.Component interface.
func (c *kubeComponent) ID() dependency.Instance {
	return dependency.Citadel
}

// Requires implements the component.Component interface.
func (c *kubeComponent) Requires() []dependency.Instance {
	return make([]dependency.Instance, 0)
}

// Init implements the component.Component interface.
func (c *kubeComponent) Init(ctx environment.ComponentContext, deps map[dependency.Instance]interface{}) (interface{}, error) {
	e, ok := ctx.Environment().(*kubernetes.Implementation)
	if !ok {
		return nil, fmt.Errorf("unsupported environment: %q", ctx.Environment().EnvironmentID())
	}

	result, err := c.doInit(e)
	if err != nil {
		return nil, multierror.Prefix(err, "citadel init failed:")
	}
	return result, nil
}

func (c *kubeComponent) doInit(e *kubernetes.Implementation) (interface{}, error) {
	res := &deployedCitadel{
		local: false,
	}

	s := e.Accessor.GetSecret(e.KubeSettings().IstioSystemNamespace)
	res.secret = s
	return res, nil
}

func getGrpcPort(e *kubernetes.Implementation) (uint16, error) {
	svc, err := e.Accessor.GetService(e.KubeSettings().IstioSystemNamespace, citadelService)
	if err != nil {
		return 0, fmt.Errorf("failed to retrieve service %s: %v", citadelService, err)
	}
	for _, portInfo := range svc.Spec.Ports {
		if portInfo.Name == grpcPortName {
			return uint16(portInfo.TargetPort.IntValue()), nil
		}
	}
	return 0, fmt.Errorf("failed to get target port in service %s", citadelService)
}

type deployedCitadel struct {
	// Indicates that the component is running in local mode.
	local bool

	secret cv1.SecretInterface
}

// WaitForSecretExist waits for Citadel to create and mount secrets.
func (d *deployedCitadel) WaitForSecretExist() (*v1.Secret, error) {
	watch, err := d.secret.Watch(mv1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set up watch for secret (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			secret := event.Object.(*v1.Secret)
			if secret.GetName() == secretName {
				return secret, nil
			}
		case <-time.After(secretWaitTime - time.Since(startTime)):
			return nil, fmt.Errorf("secret %v/%v did not become existent within %v",
				secretName, secretWaitTime)
		}
	}
}

// DeleteSecret deletes a secret.
func (d *deployedCitadel) DeleteSecret() error {
	var immediate int64
	return d.secret.Delete(secretName, &mv1.DeleteOptions{GracePeriodSeconds: &immediate})
}
