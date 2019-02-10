//  Copyright 2019 Istio Authors
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

package mixer

import (
	"testing"

	"github.com/gogo/googleapis/google/rpc"
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/environment/native"
	"istio.io/istio/pkg/test/framework2/components/galley"
	"istio.io/istio/pkg/test/framework2/resource"
	"istio.io/istio/pkg/test/framework2/runtime"
	istioMixerV1 "istio.io/api/mixer/v1"

)

type Instance interface {
	Report(t testing.TB, attributes map[string]interface{})
	Check(t testing.TB, attributes map[string]interface{}) CheckResponse

	// TODO(nmittler): Remove this
	Configure(t testing.TB, yaml string)
}

// CheckResponse that is returned from a Mixer Check call.
type CheckResponse struct {
	Raw *istioMixerV1.CheckResponse
}

// Succeeded returns true if the precondition check was successful.
func (c *CheckResponse) Succeeded() bool {
	return c.Raw.Precondition.Status.Code == int32(rpc.OK)
}

func New(s resource.Context, g galley.Instance) (Instance, error) {
	switch s.Environment().Name() {
	case native.Name:
		return newNative(s, s.Environment().(*native.Environment)), nil
	default:
		return nil, environment.UnsupportedEnvironment(s.Environment().Name())
	}
}

func NewOrFail(c *runtime.TestContext, g galley.Instance) Instance {
	i, err := New(c, g)
	if err != nil {
		c.T().Fatalf("Error creating Mixer: %v", err)
	}

	return i
}
