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

	"istio.io/api/annotation"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/istio/pkg/test/util/assert"
)

func Test_serviceFilter(t *testing.T) {
	tests := []struct {
		name string
		old  *v1.Service
		cur  *v1.Service
		want bool
	}{
		{
			name: "add service export to none",
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: "~",
					},
					Name: "svc",
				},
			},
			want: true,
		},
		{
			name: "add service export to .",
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: ".",
					},
					Name: "svc",
				},
			},
			want: false,
		},
		{
			name: "add service without exportTO",
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
					Name:        "svc",
				},
			},
			want: false,
		},
		{
			name: "update exportTo from none to .",
			old: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: "~",
					},
					Name: "svc",
				},
			},
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: ".",
					},
					Name: "svc",
				},
			},
			want: false,
		},
		{
			name: "update exportTo from . to none",
			old: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: ".",
					},
					Name: "svc",
				},
			},
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: "~",
					},
					Name: "svc",
				},
			},
			want: false,
		},
		{
			name: "update service spec",
			old: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: "~",
					},
					Name: "svc",
				},
			},
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: "~",
					},
					Name: "svc",
				},
			},
			want: true,
		},
		{
			name: "delete svc export to none",
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: "~",
					},
					Name: "svc",
				},
			},
			want: true,
		},
		{
			name: "delete svc export to .",
			cur: &v1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.NetworkingExportTo.Name: ".",
					},
					Name: "svc",
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := serviceFilter(tt.old, tt.cur); got != tt.want {
				assert.Equal(t, got, tt.want)
			}
		})
	}
}
