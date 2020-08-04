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
package inject

import (
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/api/resource"

	corev1 "k8s.io/api/core/v1"
)

func TestApplyConcurrency(t *testing.T) {
	tests := []struct {
		name string
		// containers before injection.
		original []corev1.Container
		// containers after injection.
		want []corev1.Container
	}{
		{
			name: "apply concurrency with resource limit",
			original: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
			want: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "--concurrency", "2"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
		},
		{
			name: "apply concurrency with resource request",
			original: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
			want: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "--concurrency", "1"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
		},
		{
			name: "no concurrency without cpu resource request/limit",
			original: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo"},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
			want: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo"},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
		},
		{
			name: "--concurrency 4 already set",
			original: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "--concurrency", "4"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
			want: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "--concurrency", "4"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
		},
		{
			name: "--concurrency=4 already set",
			original: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "--concurrency=4"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
			want: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "--concurrency=4"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
		},
		{
			name: "-concurrency=4 already set",
			original: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "-concurrency=4"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
			want: []corev1.Container{
				{
					Name: "istio-proxy",
					Args: []string{"--foo", "-concurrency=4"},
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("1G"),
							corev1.ResourceCPU:    resource.MustParse("1000m"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceMemory: resource.MustParse("2G"),
							corev1.ResourceCPU:    resource.MustParse("1500m"),
						},
					},
				},
				{
					Name: "app",
					Args: []string{"--foo", "--bar"},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			applyConcurrency(tc.original)
			if !reflect.DeepEqual(tc.original, tc.want) {
				t.Errorf("[%v] failed, want %+v, got %+v", tc.name, tc.want, tc.original)
			}
		})
	}
}
