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
	"net"
	"testing"

	"github.com/gogo/googleapis/google/rpc"

	istioMixerV1 "istio.io/api/mixer/v1"
	"istio.io/istio/pkg/test/framework2/components/environment"
	"istio.io/istio/pkg/test/framework2/components/environment/native"
	"istio.io/istio/pkg/test/framework2/components/galley"
	"istio.io/istio/pkg/test/framework2/resource"
)

type Instance interface {
	Report(t testing.TB, attributes map[string]interface{})
	Check(t testing.TB, attributes map[string]interface{}) CheckResponse
	GetCheckAddress() net.Addr
	GetReportAddress() net.Addr
}

// CheckResponse that is returned from a Mixer Check call.
type CheckResponse struct {
	Raw *istioMixerV1.CheckResponse
}

type Config struct {
	Galley galley.Instance
}

// Succeeded returns true if the precondition check was successful.
func (c *CheckResponse) Succeeded() bool {
	return c.Raw.Precondition.Status.Code == int32(rpc.OK)
}

func New(c resource.Context, config *Config) (Instance, error) {
	switch c.Environment().EnvironmentName() {
	case environment.Native:
		return newNative(c, c.Environment().(*native.Environment), config)
	default:
		return nil, environment.UnsupportedEnvironment(c.Environment().EnvironmentName())
	}
}

func NewOrFail(t *testing.T, c resource.Context, config *Config) Instance {
	i, err := New(c, config)
	if err != nil {
		t.Fatalf("Error creating Mixer: %v", err)
	}
	return i
}
