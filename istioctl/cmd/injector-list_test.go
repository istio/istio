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

package cmd

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
)

func Test_injectionDisabled(t *testing.T) {
	type args struct {
		pod *v1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "disable inject",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{annotation.SidecarInject.Name: "false"},
					},
				},
			},
			want: true,
		},
		{
			name: "disable inject, fold equals",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{annotation.SidecarInject.Name: "False"},
					},
				},
			},
			want: true,
		},
		{
			name: "disable inject, fold equals",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{annotation.SidecarInject.Name: "False"},
					},
				},
			},
			want: true,
		},
		{
			name: "empty annotation",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
			want: false,
		},
		{
			name: "nil input",
			args: args{
				&v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: nil,
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := injectionDisabled(tt.args.pod); got != tt.want {
				t.Errorf("injectionDisabled() = %v, want %v", got, tt.want)
			}
		})
	}
}
