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

package pilot

import (
	"fmt"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/framework/resource"
)

// Instance of Pilot
type Instance interface {
	resource.Resource
}

// Structured config for the Pilot component
type Config struct {
	fmt.Stringer

	// Cluster to be used in a multicluster environment
	Cluster resource.Cluster
}

// New returns a new instance of echo.
func New(ctx resource.Context, cfg Config) (i Instance, err error) {
	return newKube(ctx, cfg)
}

// NewOrFail returns a new Pilot instance, or fails test.
func NewOrFail(t test.Failer, c resource.Context, config Config) Instance {
	i, err := New(c, config)
	if err != nil {
		t.Fatalf("pilot.NewOrFail: %v", err)
	}
	return i
}
