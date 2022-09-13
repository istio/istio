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

package echo

import (
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/echo"
)

// CallResult the result of a call operation.
type CallResult struct {
	From      Caller
	Opts      CallOptions
	Responses echo.Responses
}

type Caller interface {
	// Call from this Instance to a target Instance.
	Call(options CallOptions) (CallResult, error)
	CallOrFail(t test.Failer, options CallOptions) CallResult
}

type Callers []Caller

// Instances returns an Instances if all callers are Instance, otherwise returns nil.
func (c Callers) Instances() Instances {
	var out Instances
	for _, caller := range c {
		c, ok := caller.(Instance)
		if !ok {
			return nil
		}
		out = append(out, c)
	}
	return out
}
