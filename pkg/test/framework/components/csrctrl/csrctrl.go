// Copyright Istio Authors
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

package csrctrl

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/components/cluster"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance represents a deployed CSR controller instance in a Kubernetes cluster.
type Instance interface {
	resource.Resource
	// Gets the namespace in which redis is deployed.
	GetSignerNamespace() string
}

type Config struct {
	// Which KubeConfig should be used in a multicluster environment
	Cluster cluster.Cluster

	// SignerNames indicate signer names for custome CA, support multiple signer names splitted by ','
	SignerNames string

	// Path to CA files, including xxx.csr, xxx.pem and xxx.key
	SignerRootPath string
}

// New returns a new instance of CSR controller.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new CSR controller instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("CSR Controller.NewOrFail: %v", err)
	}

	return i
}
