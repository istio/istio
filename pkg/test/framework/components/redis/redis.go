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

package redis

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment"
	"istio.io/istio/pkg/test/framework/resource"
)

// Redis represents a deployed Redis app instance in a Kubernetes cluster.
type Instance interface {
	// Gets the namespace in which redis is deployed.
	GetRedisNamespace() string
}

// New returns a new instance of redis.
func New(ctx resource.Context) (i Instance, err error) {
	err = resource.UnsupportedEnvironment(ctx.Environment())
	ctx.Environment().Case(environment.Kube, func() {
		i, err = newKube(ctx)
	})
	return
}

// NewOrFail returns a new Redis instance or fails test.
func NewOrFail(t *testing.T, ctx resource.Context) Instance {
	t.Helper()
	i, err := New(ctx)
	if err != nil {
		t.Fatalf("redis.NewOrFail: %v", err)
	}

	return i
}
