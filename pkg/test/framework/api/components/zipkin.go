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

package components

import (
	"testing"

	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
)

// Zipkin represents a deployed Zipkin instance.
type Zipkin interface {
	component.Instance

	// Returns the address the zipkin server is running on.
	GetAddress() string

	// Returns a list of all service names associated with span endpoints.
	ListServices() ([]string, error)

	// Returns a list of traces matching the given trace id.
	GetTracesById(traceid string) ([][]map[string]interface{}, error)
}

// GetZipkin from the repository
func GetZipkin(e component.Repository, t testing.TB) Zipkin {
	return e.GetComponentOrFail(ids.Zipkin, t).(Zipkin)
}
