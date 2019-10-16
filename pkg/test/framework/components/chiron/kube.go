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

package chiron

import (
	"fmt"
	"time"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/environment/kube"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"

	v1 "k8s.io/api/core/v1"
	mv1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cv1 "k8s.io/client-go/kubernetes/typed/core/v1"
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
	c.secret = env.GetSecret(c.istio.Settings().IstioNamespace)

	return c
}

func (c *kubeComponent) ID() resource.ID {
	return c.id
}

func (c *kubeComponent) WaitForSecretToExist(name string, waitTime time.Duration) (*v1.Secret, error) {
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
			if secret.GetName() == name {
				return secret, nil
			}
		case <-time.After(waitTime - time.Since(startTime)):
			return nil, fmt.Errorf("secret %v did not become existent within %v",
				name, waitTime)
		}
	}
}

func (c *kubeComponent) WaitForSecretToExistOrFail(t test.Failer, name string, waitTime time.Duration) *v1.Secret {
	t.Helper()
	s, err := c.WaitForSecretToExist(name, waitTime)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
