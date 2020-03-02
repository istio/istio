// Copyright 2020 Istio Authors
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

package mesh

import (
	"fmt"
	"strings"
	"testing"
)

func TestValidateSetFlags(t *testing.T) {

	tests := []struct {
		name string
		args []string
		want error
	}{
		{
			name: "Test when no flag prams in sent",
			args: []string{},
			want: nil,
		},
		{
			name: "Test invalid flag format",
			args: []string{
				"values.global.sds.enabled",
			},
			want: fmt.Errorf("\n Invalid flag format %q", "values.global.sds.enabled"),
		},
		{
			name: "Test valid flag format",
			args: []string{
				"values.global.sds.enabled=true",
			},
			want: nil,
		},
		{
			name: "Test flag name not available",
			args: []string{
				"values.global.controlPlaneSecurity=true",
			},
			want: fmt.Errorf("\n Invalid flag: %q", "values.global.controlPlaneSecurity"),
		},
		{
			name: "Test flag name available",
			args: []string{
				"values.global.controlPlaneSecurityEnabled=true",
			},
			want: nil,
		},
		{
			name: "Test Unsupported values",
			args: []string{
				"values.global.imagePullPolicy=Occassionally",
			},
			want: fmt.Errorf("\n Unsupported value: %q, supported values for: %q is %q",
				"Occassionally", "imagePullPolicy", strings.Join(imagePullPolicy, ", ")),
		},
		{
			name: "Test supported values",
			args: []string{
				"values.global.imagePullPolicy=IfNotPresent",
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ValidateSetFlags(tt.args)
			if got != nil && fmt.Sprintf("%v", got) != fmt.Sprintf("%v", tt.want) {
				t.Errorf("got: %v, want: %v", got, tt.want)
			}
		})
	}
}
