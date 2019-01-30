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

	"github.com/gogo/googleapis/google/rpc"

	mixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/pkg/test/framework/api/component"
	"istio.io/istio/pkg/test/framework/api/ids"
	"istio.io/istio/pkg/test/framework/api/lifecycle"
)

// Mixer represents a deployed Mixer instance.
type Mixer interface {
	component.Instance

	// Report is called directly with the given attributes.
	Report(t testing.TB, attributes map[string]interface{})
	Check(t testing.TB, attributes map[string]interface{}) CheckResponse

	// TODO(nmittler): Remove this
	Configure(t testing.TB, scope lifecycle.Scope, yaml string)
}

// CheckResponse that is returned from a Mixer Check call.
type CheckResponse struct {
	Raw *mixerV1.CheckResponse
}

// Succeeded returns true if the precondition check was successful.
func (c *CheckResponse) Succeeded() bool {
	return c.Raw.Precondition.Status.Code == int32(rpc.OK)
}

// GetMixer from the repository
func GetMixer(e component.Repository, t testing.TB) Mixer {
	return e.GetComponentOrFail("", ids.Mixer, t).(Mixer)
}
