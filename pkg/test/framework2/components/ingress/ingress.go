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
	"istio.io/istio/pkg/test/framework2/components/istio"
	"testing"

	"istio.io/istio/pkg/test/framework2/core"
)

// Instance represents a deployed Ingress Gateway instance.
type Instance interface {
	core.Resource

	// Address returns the external HTTP address of the ingress gateway (or the NodePort address,
	// when running under Minikube).
	Address() string

	//  Call makes an HTTP call through ingress, where the URL has the given path.
	Call(path string) (IngressCallResponse, error)
}

type Config struct {
	Istio istio.Instance
}

// IngressCallResponse is the result of a call made through Istio Ingress.
type IngressCallResponse struct {
	// Response status code
	Code int

	// Response body
	Body string
}

// New returns a new Ingress instance.
func New(ctx core.Context, cfg Config) (Instance, error) {
	switch ctx.Environment().EnvironmentName() {
	case core.Kube:
		return newKube(ctx, cfg)
	default:
		return nil, core.UnsupportedEnvironment(ctx.Environment().EnvironmentName())
	}
}

// New returns a new Ingress instance or fails test
func NewOrFail(t *testing.T, ctx core.Context, cfg Config) Instance {
	t.Helper()
	i, err := New(ctx, cfg)
	if err != nil {
		t.Fatalf("prometheus.NewOrFail: %v", err)
	}

	return i
}
