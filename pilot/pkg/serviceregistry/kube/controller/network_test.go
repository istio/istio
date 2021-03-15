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

package controller

import (
	"testing"

	"istio.io/istio/pilot/pkg/model"
)

func TestDidGatewaysChange(t *testing.T) {
	tests := map[string]struct {
		a       []*model.Gateway
		b       []*model.Gateway
		changed bool
	}{
		"same": {
			a:       []*model.Gateway{{Addr: "1.2.3.4", Port: 15443}},
			b:       []*model.Gateway{{Addr: "1.2.3.4", Port: 15443}},
			changed: false,
		},
		"different addr": {
			a:       []*model.Gateway{{Addr: "1.2.3.4", Port: 15443}},
			b:       []*model.Gateway{{Addr: "1.2.3.5", Port: 15443}},
			changed: true,
		},
		"different port": {
			a:       []*model.Gateway{{Addr: "1.2.3.4", Port: 15443}},
			b:       []*model.Gateway{{Addr: "1.2.3.5", Port: 15444}},
			changed: true,
		},
		"different length": {
			a:       []*model.Gateway{},
			b:       []*model.Gateway{{Addr: "1.2.3.5", Port: 15444}},
			changed: true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			if got := didGatewaysChange(tc.a, tc.b); got != tc.changed {
				t.Errorf("comparing %v and %v, got %v but wanted %v", tc.a, tc.b, got, tc.changed)
			}
			if got := didGatewaysChange(tc.b, tc.a); got != tc.changed {
				t.Errorf("comparing %v and %v, got %v but wanted %v", tc.b, tc.a, got, tc.changed)
			}
		})
	}
}
