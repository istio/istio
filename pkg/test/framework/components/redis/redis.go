//  Copyright Istio Authors
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

package redis

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
)

// Redis represents a deployed Redis app instance in a Kubernetes cluster.
type Instance interface {
	// Gets the namespace in which redis is deployed.
	GetRedisNamespace() string
}

type Config struct {
	// Which KubeConfig should be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of redis.
func New(ctx resource.Context, c Config) (i Instance, err error) {
	return newKube(ctx, c)
}

// NewOrFail returns a new Redis instance or fails test.
func NewOrFail(t test.Failer, ctx resource.Context, c Config) Instance {
	t.Helper()
	i, err := New(ctx, c)
	if err != nil {
		t.Fatalf("redis.NewOrFail: %v", err)
	}

	return i
}
