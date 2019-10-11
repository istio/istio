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

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/components/namespace"
	"istio.io/istio/pkg/test/framework/resource"

	v1 "k8s.io/api/core/v1"
	mv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// Specifies how long we wait before a secret becomes existent.
	secretWaitTime = 60 * time.Second
	// Name of secret created by Citadel
	SecretName = "istio.default"
)

var _ Instance = &kubeComponent{}

type kubeComponent struct {
	id    resource.ID
	istio istio.Instance
	env   *kube.Environment
}

func newKube(ctx resource.Context, cfg Config) Instance {
	c := &kubeComponent{
		istio: cfg.Istio,
		env:   ctx.Environment().(*kube.Environment),
	}
	c.id = ctx.TrackResource(c)

	return c
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) WaitForSecretToExist(ns namespace.Instance, name string) (*v1.Secret, error) {
	watch, err := c.env.GetSecret(ns.Name()).Watch(mv1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set up watch for secret (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			secret := event.Object.(*v1.Secret)
			if secret.GetName() == name {
				return secret, nil
			}
		case <-time.After(secretWaitTime - time.Since(startTime)):
			return nil, fmt.Errorf("secret %v did not become existent within %v",
				name, secretWaitTime)
		}
	}
}

func (c *kubeComponent) WaitForSecretToExistOrFail(t test.Failer, ns namespace.Instance, name string) *v1.Secret {
	t.Helper()
	s, err := c.WaitForSecretToExist(ns, name)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (c *kubeComponent) DeleteSecret(ns namespace.Instance, name string) error {
	var immediate int64
	return c.env.GetSecret(ns.Name()).Delete(name, &mv1.DeleteOptions{GracePeriodSeconds: &immediate})
}

func (c *kubeComponent) DeleteSecretOrFail(t test.Failer, ns namespace.Instance, name string) {
	t.Helper()
	if err := c.DeleteSecret(ns, name); err != nil {
		t.Fatal(err)
	}
}
