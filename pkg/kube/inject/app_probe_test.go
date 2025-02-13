// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestFindSidecar(t *testing.T) {
	proxy := corev1.Container{Name: "istio-proxy"}
	app := corev1.Container{Name: "app"}
	for _, tc := range []struct {
		name       string
		containers *corev1.Pod
		want       *corev1.Container
	}{
		{
			name:       "only-sidecar",
			containers: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{proxy}}},
			want:       &proxy,
		},
		{
			name:       "app-and-sidecar",
			containers: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{app, proxy}}},
			want:       &proxy,
		},
		{
			name:       "no-sidecar",
			containers: &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{app}}},
			want:       nil,
		},
		{
			name:       "init-sidecar",
			containers: &corev1.Pod{Spec: corev1.PodSpec{InitContainers: []corev1.Container{proxy}, Containers: []corev1.Container{app}}},
			want:       &proxy,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, FindSidecar(tc.containers), tc.want)
		})
	}
}

func TestShouldRewriteAppHTTPProbers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		specSetting bool
		annotations map[string]string
		expected    bool
	}{
		{
			name:        "RewriteAppHTTPProbe-set-in-annotations",
			specSetting: false,
			annotations: nil,
			expected:    false,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-annotations",
			specSetting: true,
			annotations: nil,
			expected:    true,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-sidecar-injection-spec",
			specSetting: false,
			annotations: map[string]string{},
			expected:    false,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-sidecar-injection-spec",
			specSetting: true,
			annotations: map[string]string{},
			expected:    true,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-annotations",
			specSetting: false,
			annotations: map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "true"},
			expected:    true,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-sidecar-injection-spec-&-annotations",
			specSetting: true,
			annotations: map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "true"},
			expected:    true,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-annotations",
			specSetting: false,
			annotations: map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "false"},
			expected:    false,
		},
		{
			name:        "RewriteAppHTTPProbe-set-in-sidecar-injection-spec-&-annotations",
			specSetting: true,
			annotations: map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "false"},
			expected:    false,
		},
	} {
		got := ShouldRewriteAppHTTPProbers(tc.annotations, tc.specSetting)
		want := tc.expected
		if got != want {
			t.Errorf("[%v] failed, want %v, got %v", tc.name, want, got)
		}
	}
}

func TestDumpAppGRPCProbers(t *testing.T) {
	svc := "foo"
	for _, tc := range []struct {
		name     string
		pod      *corev1.Pod
		expected string
	}{
		{
			name: "simple gRPC liveness probe",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "foo",
						LivenessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								GRPC: &corev1.GRPCAction{
									Port: 1234,
								},
							},
						},
					},
				},
			}},
			expected: `
{
    "/app-health/foo/livez": {
        "grpc": {
            "port": 1234,
            "service": null
        }
    }
}`,
		},
		{
			name: "gRPC readiness probe with service",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "bar",
						ReadinessProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								GRPC: &corev1.GRPCAction{
									Port:    1234,
									Service: &svc,
								},
							},
						},
					},
				},
			}},
			expected: `
{
    "/app-health/bar/readyz": {
        "grpc": {
            "port": 1234,
            "service": "foo"
        }
    }
}`,
		},
		{
			name: "gRPC startup probe with service and timeout including a http lifecycle handler",
			pod: &corev1.Pod{Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "foo",
						StartupProbe: &corev1.Probe{
							ProbeHandler: corev1.ProbeHandler{
								GRPC: &corev1.GRPCAction{
									Port:    1234,
									Service: &svc,
								},
							},
							TimeoutSeconds: 10,
						},
						Lifecycle: &corev1.Lifecycle{
							PreStop: &corev1.LifecycleHandler{
								HTTPGet: &corev1.HTTPGetAction{
									Path: "/foo",
									Port: intstr.IntOrString{
										IntVal: 1234,
									},
									Host:   "foo",
									Scheme: "HTTP",
								},
							},
						},
					},
				},
			}},
			expected: `
{
  "/app-health/foo/startupz": {
    "grpc": {
      "port": 1234,
      "service": "foo"
    },
    "timeoutSeconds": 10
  },
  "/app-lifecycle/foo/prestopz": {
    "httpGet": {
      "path": "/foo",
      "port": 1234,
      "host": "foo",
      "scheme": "HTTP"
    }
  }
}`,
		},
	} {
		got := DumpAppProbers(tc.pod, 15020)
		test.JSONEquals(t, got, tc.expected)
	}
}

func TestPatchRewriteProbe(t *testing.T) {
	svc := "foo"
	annotations := map[string]string{}
	statusPort := intstr.FromInt32(15020)
	for _, tc := range []struct {
		name        string
		pod         *corev1.Pod
		annotations map[string]string
		expected    *corev1.Pod
	}{
		{
			name: "pod with no probes",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
						},
					},
				},
			},
			expected: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
						},
					},
				},
			},
		},
		{
			name: "pod with a gRPC liveness probe",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port: 1234,
									},
								},
							},
						},
					},
				},
			},
			expected: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/app-health/foo/livez",
										Port: statusPort,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "pod with gRPC liveness,readiness,startup probes",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    1234,
										Service: &svc,
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    1235,
										Service: &svc,
									},
								},
								TimeoutSeconds: 10,
							},
						},
						{
							Name: "bar",
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port: 1236,
									},
								},
								TimeoutSeconds:      20,
								PeriodSeconds:       10,
								InitialDelaySeconds: 10,
							},
						},
					},
				},
			},
			expected: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/app-health/foo/livez",
										Port: statusPort,
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/app-health/foo/readyz",
										Port: statusPort,
									},
								},
								TimeoutSeconds: 10,
							},
						},
						{
							Name: "bar",
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/app-health/bar/startupz",
										Port: statusPort,
									},
								},
								TimeoutSeconds:      20,
								PeriodSeconds:       10,
								InitialDelaySeconds: 10,
							},
						},
					},
				},
			},
		},
	} {
		patchRewriteProbe(annotations, tc.pod, 15020)
		if !reflect.DeepEqual(tc.pod, tc.expected) {
			t.Errorf("[%v] failed, want %v, got %v", tc.name, tc.expected, tc.pod)
		}
	}
}
