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

/*
NOTICE: The zsh constants are derived from the kubectl completion code
(k8s.io/kubernetes/pkg/kubectl/cmd/completion/completion.go), with the
following copyright/license:

Copyright 2016 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package collateral

import (
	"reflect"
	"testing"
)

func Test_buildNestedMap(t *testing.T) {
	type args struct {
		flatMap map[string]string
	}
	tests := []struct {
		name       string
		args       args
		wantResult map[string]any
	}{
		{
			name: "configmap",
			args: args{
				flatMap: map[string]string{
					"one.two.valuethree":      "onetwovaluethree",
					"one.two.three.valuefour": "onetwothreevaluefour",
					"extra":                   "thing",
				},
			},
			wantResult: map[string]any{
				"one": map[string]any{
					"two": map[string]any{
						"valuethree": "onetwovaluethree",
						"three": map[string]any{
							"valuefour": "onetwothreevaluefour",
						},
					},
				},
				"extra": "thing",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if gotResult := buildNestedMap(tt.args.flatMap); !reflect.DeepEqual(gotResult, tt.wantResult) {
				t.Errorf("buildNestedMap() = %v, want %v", gotResult, tt.wantResult)
			}
		})
	}
}
