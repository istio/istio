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

package egress

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"

	"istio.io/istio/pkg/test/framework/resource"

	v1 "k8s.io/api/core/v1"
)

// Egress represents a deployed Egress Gateway instance.
type Egress interface {
	resource.Resource

	// Configure a secret and wait for the existence
	ConfigureSecretAndWaitForExistence(secret *v1.Secret) (*v1.Secret, error)

	// Add addition secret mountpoint
	AddSecretMountPoint(path string) error
}

type Config struct {
	Istio istio.Instance
}

// Deploy returns a new instance of an egress.
func New(ctx resource.Context, cfg Config) (i Egress, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Kube, func() {
		i, err = NewKubeComponent(ctx, cfg)
	})
	return
}

// Deploy returns a new Ingress instance or fails test
func NewOrFail(t *testing.T, ctx resource.Context, cfg Config) Egress {
	t.Helper()
	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("egress.NewOrFail: %v", err)
	}

	return i
}
