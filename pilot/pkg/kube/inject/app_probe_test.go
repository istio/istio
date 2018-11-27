// Copyright 2018 Istio Authors
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

	"github.com/gogo/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestRewriteAppHTTPProbe(t *testing.T) {
	tests := []struct {
		name    string
		sidecar *SidecarInjectionSpec
		// PodSpec before injection.
		original *corev1.PodSpec
		// PodSpec after injection.
		want *corev1.PodSpec
	}{
		{
			name: "empty-spec",
		},
		{
			name: "one-container",
			sidecar: &SidecarInjectionSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
				},
			},
			original: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
				},
			},
			want: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "8000"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "both-readiness-liveness-rewrite",
			sidecar: &SidecarInjectionSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
				},
			},
			original: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
						LivenessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/live",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
				},
			},
			want: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "8000"},
									},
								},
							},
						},
						LivenessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/live",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "8000"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "no-statusPort-find",
			sidecar: &SidecarInjectionSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusXXXX", "15020"},
					},
				},
			},
			original: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusXXXX", "15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
				},
			},
			want: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusXXXX", "15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
				},
			},
		},
		{
			name: "-statusPort=15020-parsing",
			sidecar: &SidecarInjectionSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "-statusPort=15020"},
					},
				},
			},
			original: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "-statusPort=15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
				},
			},
			want: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "-statusPort=15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "8000"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "--statusPort=15020-parsing",
			sidecar: &SidecarInjectionSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
				},
			},
			original: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort=15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
				},
			},
			want: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort=15020"},
					},
					{
						Name: "app",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "8000"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "two-container-rewrite",
			sidecar: &SidecarInjectionSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
				},
			},
			original: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
					{
						Name: "app1",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(8000),
								},
							},
						},
					},
					{
						Name: "app2",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(9000),
								},
							},
						},
					},
				},
			},
			want: &corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "istio-proxy",
						Args: []string{"--foo", "--statusPort", "15020"},
					},
					{
						Name: "app1",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "8000"},
									},
								},
							},
						},
					},
					{
						Name: "app2",
						ReadinessProbe: &corev1.Probe{
							Handler: corev1.Handler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/ready",
									Port: intstr.FromInt(15020),
									HTTPHeaders: []corev1.HTTPHeader{
										{Name: "istio-app-probe-port", Value: "9000"},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		pod := proto.Clone(tc.original).(*corev1.PodSpec)
		rewriteAppHTTPProbe(tc.sidecar, pod)
		if !reflect.DeepEqual(pod, tc.want) {
			t.Errorf("[%v] failed, want %+v, got %+v", tc.name, tc.want, pod)
		}
	}
}
