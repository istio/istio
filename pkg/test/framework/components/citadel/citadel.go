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

package citadel

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/components/istio"
	"istio.io/istio/pkg/test/framework/resource"
)

// Citadel represents a deployed Citadel instance.
type Instance interface {
	resource.Resource

	WaitForSecretToExist() (*corev1.Secret, error)
	DeleteSecret() error
}

type Config struct {
	Istio istio.Instance
}

// Deploy returns a new instance of Apps
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())

	ctx.Environment().Case(environment.Kube, func() {
		i = newKube(ctx, cfg)
		err = nil
	})

	return
}

// Deploy returns a new instance of Citadel or fails test
func NewOrFail(t *testing.T, ctx resource.Context, cfg Config) Instance {
	t.Helper()

	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("citadel.NewOrFail: %v", err)
	}
	return i
}
