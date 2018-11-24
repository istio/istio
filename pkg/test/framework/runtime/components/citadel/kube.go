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
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/components"
	"istio.io/istio/pkg/test/framework/api/context"
	"istio.io/istio/pkg/test/framework/api/descriptors"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
	"istio.io/istio/pkg/test/framework/runtime/api"
	"istio.io/istio/pkg/test/framework/runtime/components/environment/kube"

	"time"

	"k8s.io/api/core/v1"
	mv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

const (
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
	// Name of secret created by Citadel
	secretName = "istio.default"
)

var _ components.Citadel = &kubeComponent{}

type kubeComponent struct {
	scope lifecycle.Scope

	secret cv1.SecretInterface
}

// NewKubeComponent factory function for the component
func NewKubeComponent() (api.Component, error) {
	return &kubeComponent{}, nil
}

func (c *kubeComponent) Descriptor() component.Descriptor {
	return descriptors.Citadel
}

func (c *kubeComponent) Scope() lifecycle.Scope {
	return c.scope
}

func (c *kubeComponent) Start(ctx context.Instance, scope lifecycle.Scope) (err error) {
	c.scope = scope

	env, err := kube.GetEnvironment(ctx)
	if err != nil {
		return err
	}

	c.secret = env.GetSecret(env.SystemNamespace())
	return nil
}

func (c *kubeComponent) WaitForSecretToExist() (*v1.Secret, error) {
	watch, err := c.secret.Watch(mv1.ListOptions{})
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
			return nil, fmt.Errorf("secret %v did not become existent within %v",
				secretName, secretWaitTime)
		}
	}
}

func (c *kubeComponent) DeleteSecret() error {
	var immediate int64
	return c.secret.Delete(secretName, &mv1.DeleteOptions{GracePeriodSeconds: &immediate})
}
