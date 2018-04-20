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

package test

import (
	"testing"

	"istio.io/istio/pkg/test/cluster"
	"istio.io/istio/pkg/test/local"
)

// GetClusterEnvironment a convenience method for constructing a cluster environment.
func GetClusterEnvironment(t testing.TB) *cluster.Environment {
	return &cluster.Environment{
		T: t,
	}
}

// GetLocalEnvironment a convenience method for constructing a local environment.
func GetLocalEnvironment(t testing.TB) *local.Environment {
	return &local.Environment{
		T: t,
	}
}
