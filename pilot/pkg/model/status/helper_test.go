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

package status

import (
	"reflect"
	"testing"

	"istio.io/api/meta/v1alpha1"
)

func Test_updateCondition(t *testing.T) {
	type args struct {
		conditions []*v1alpha1.IstioCondition
		condition  *v1alpha1.IstioCondition
	}
	tests := []struct {
		name string
		args args
		want []*v1alpha1.IstioCondition
	}{
		{
			name: "Update conditions",
			args: args{
				conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "Reconciled",
						Status:  "False",
						Message: "1/2 proxies up to date.",
					},
				},
				condition: &v1alpha1.IstioCondition{
					Type:    "PassedValidation",
					Status:  "False",
					Message: "just a update test, here",
				},
			},
			want: []*v1alpha1.IstioCondition{
				{
					Type:    "PassedValidation",
					Status:  "False",
					Message: "just a update test, here",
				},
				{
					Type:    "Reconciled",
					Status:  "False",
					Message: "1/2 proxies up to date.",
				},
			},
		},
		{
			name: "New conditions",
			args: args{
				conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "Reconciled",
						Status:  "False",
						Message: "1/2 proxies up to date.",
					},
				},
				condition: &v1alpha1.IstioCondition{
					Type:    "SomeRandomType",
					Status:  "True",
					Message: "just a new condition",
				},
			},
			want: []*v1alpha1.IstioCondition{
				{
					Type:    "PassedValidation",
					Status:  "True",
					Message: "just a test, here",
				},
				{
					Type:    "Reconciled",
					Status:  "False",
					Message: "1/2 proxies up to date.",
				},
				{
					Type:    "SomeRandomType",
					Status:  "True",
					Message: "just a new condition",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := updateCondition(tt.args.conditions, tt.args.condition); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("updateCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deleteCondition(t *testing.T) {
	type args struct {
		conditions []*v1alpha1.IstioCondition
		condition  string
	}
	tests := []struct {
		name string
		args args
		want []*v1alpha1.IstioCondition
	}{
		{
			name: "Delete conditions",
			args: args{
				conditions: []*v1alpha1.IstioCondition{
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here",
					},
					{
						Type:    "PassedValidation",
						Status:  "True",
						Message: "just a test, here2",
					},
					{
						Type:    "Reconciled",
						Status:  "False",
						Message: "1/2 proxies up to date.",
					},
				},
				condition: "PassedValidation",
			},
			want: []*v1alpha1.IstioCondition{
				{
					Type:    "Reconciled",
					Status:  "False",
					Message: "1/2 proxies up to date.",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deleteCondition(tt.args.conditions, tt.args.condition); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deleteCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}
