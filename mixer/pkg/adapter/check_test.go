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

package adapter

import (
	"testing"

	"istio.io/istio/mixer/pkg/status"
)

func TestCheckResult(t *testing.T) {

	tests := []struct {
		name           string
		vg             CheckResult
		isDefault      bool
		expectedString string
	}{
		{
			name:           "default, and no string",
			vg:             CheckResult{},
			isDefault:      true,
			expectedString: "CheckResult: status:OK, duration:0, usecount:0",
		},
		{
			name: "not default, and occur fail status",
			vg: CheckResult{
				Status: status.WithCancelled("FAIL"),
			},
			isDefault:      false,
			expectedString: "CheckResult: status:CANCELLED (FAIL), duration:0, usecount:0",
		},
		{
			name: "not default, and just set validDuration",
			vg: CheckResult{
				ValidDuration: 1,
			},
			isDefault:      false,
			expectedString: "CheckResult: status:OK, duration:1, usecount:0",
		},
		{
			name: "not default, and just validUseCount parameter",
			vg: CheckResult{
				ValidUseCount: 10,
			},
			isDefault:      false,
			expectedString: "CheckResult: status:OK, duration:0, usecount:10",
		},
	}

	for _, rt := range tests {
		t.Run(rt.name, func(t *testing.T) {

			actualDefault := rt.vg.IsDefault()
			if actualDefault != rt.isDefault {
				t.Fatalf("check result about default error, Got: %v\n Expected: %v", actualDefault, rt.isDefault)
			}

			actualString := rt.vg.String()
			if actualString != rt.expectedString {
				t.Fatalf("check result about string error, Got: %s\n Expected: %s", actualString, rt.expectedString)
			}
		})
	}
}
