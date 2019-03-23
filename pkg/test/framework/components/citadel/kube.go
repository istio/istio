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
	"time"

	v1 "k8s.io/api/core/v1"
	mv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

const (
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 20 * time.Second
	// Name of secret created by Citadel
	secretName = "istio.default"
)

var _ Instance = &kubeComponent{}

type kubeComponent struct {
	id     resource.ID
	istio  istio.Instance
	secret cv1.SecretInterface
}

func newKube(ctx resource.Context, cfg Config) Instance {
	c := &kubeComponent{
		istio: cfg.Istio,
	}
	c.id = ctx.TrackResource(c)

	env := ctx.Environment().(*kube.Environment)
	c.secret = env.GetSecret(c.istio.Settings().SystemNamespace)

	return c
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
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
