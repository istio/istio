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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
						Ports: []corev1.ContainerPort{
							{ContainerPort: 1234},
						},
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
						Ports: []corev1.ContainerPort{
							{ContainerPort: 1234},
						},
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
						Ports: []corev1.ContainerPort{
							{ContainerPort: 1234},
						},
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

func TestDumpAppProbersForIncludedPorts(t *testing.T) {
	const targetPort int32 = 15020
	svc := "foo"

	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected string
	}{
		{
			name: "simple gRPC liveness probe",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 1234},
							},
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
			expected: `{"/app-health/foo/livez":{"grpc":{"port":1234,"service":null}}}`,
		},
		{
			name: "gRPC readiness probe with service",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "bar",
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 1234,
									Protocol:      corev1.ProtocolTCP,
								},
							},
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
				},
			},
			expected: `{"/app-health/bar/readyz":{"grpc":{"port":1234,"service":"foo"}}}`,
		},
		{
			name: "gRPC startup probe with service and timeout",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 1234,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    1234,
										Service: &svc,
									},
								},
								TimeoutSeconds: 10,
							},
						},
					},
				},
			},
			expected: `{"/app-health/foo/startupz":{"grpc":{"port":1234,"service":"foo"},"timeoutSeconds":10}}`,
		},
		{
			name: "startup probe with includeInboundPorts annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "1234",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 1234,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    1234,
										Service: &svc,
									},
								},
								TimeoutSeconds: 10,
							},
						},
					},
				},
			},
			expected: `{"/app-health/foo/startupz":{"grpc":{"port":1234,"service":"foo"},"timeoutSeconds":10}}`,
		},
		{
			name: "startup probe with excludeInboundPorts annotation",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "1234",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 1234,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    1234,
										Service: &svc,
									},
								},
								TimeoutSeconds: 10,
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "startup probe with includeInboundPorts annotation and one of the ports in the list",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "foo",
							Ports: []corev1.ContainerPort{
								{
									Name:          "grpc",
									ContainerPort: 1234,
									Protocol:      corev1.ProtocolTCP,
								},
								{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port:    1234,
										Service: &svc,
									},
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/health/full",
										Host:   "localhost",
										Scheme: "http",
										Port: intstr.IntOrString{
											Type:   intstr.String,
											StrVal: "8080",
										},
									},
								},
								TimeoutSeconds: 10,
							},
						},
					},
				},
			},
			expected: `{
				"/app-health/foo/startupz":{
					"httpGet":{
						"path":"/health/full",
						"port":8080,
						"host":"localhost",
						"scheme":"http"
					},
					"timeoutSeconds":10
				}
			}`,
		},
		{
			name: "http, grpc, and tcp probes on different ports",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "multi-probe-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
								{ContainerPort: 7070},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(8080),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port: 9090,
									},
								},
							},
							StartupProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt32(7070),
									},
								},
							},
						},
					},
				},
			},
			expected: `{
				"/app-health/multi-probe-container/livez": {
					"httpGet": {
						"path": "/health",
						"port": 8080
					}
				},
				"/app-health/multi-probe-container/readyz": {
					"grpc": {
						"port": 9090,
						"service": null
					}
				},
				"/app-health/multi-probe-container/startupz": {
					"tcpSocket": {
						"port": 7070
					}
				}
			}`,
		},
		{
			name: "http liveness probe on excluded port",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "excluded-port-container",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(8080),
									},
								},
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "http liveness probe on excluded port and readinessProbe on included port",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080",
						annotation.SidecarTrafficIncludeInboundPorts.Name: "7070",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "excluded-port-container",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(8080),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(7070),
									},
								},
							},
						},
					},
				},
			},
			expected: `{"/app-health/excluded-port-container/readyz":{"httpGet":{"path":"/health","port":7070}}}`,
		},
		{
			name: "liveness and readiness probes on different http ports with one included",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "partial-included-ports",
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/liveness",
										Port: intstr.FromInt32(8080),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/readiness",
										Port: intstr.FromInt32(9090),
									},
								},
							},
						},
					},
				},
			},
			expected: `{"/app-health/partial-included-ports/livez":{"httpGet":{"path":"/liveness","port":8080}}}`,
		},
		// New test cases for lifecycle hooks
		{
			name: "preStop lifecycle hook with HTTP handler",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "prestop-http-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/shutdown",
										Port:   intstr.FromInt32(8080),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
			expected: `{"/app-lifecycle/prestop-http-container/prestopz":{"httpGet":{"path":"/shutdown","port":8080,"scheme":"HTTP"}}}`,
		},
		{
			name: "postStart lifecycle hook with HTTP handler",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "poststart-http-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
							Lifecycle: &corev1.Lifecycle{
								PostStart: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/startup",
										Port:   intstr.FromInt32(8080),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
			expected: `{
				"/app-lifecycle/poststart-http-container/poststartz": {
					"httpGet": {
						"path": "/startup",
						"port": 8080,
						"scheme": "HTTP"
					}
				}
			}`,
		},
		{
			name: "both preStop and postStart lifecycle hooks",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "lifecycle-hooks-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/shutdown",
										Port:   intstr.FromInt32(8080),
										Scheme: "HTTP",
									},
								},
								PostStart: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/startup",
										Port:   intstr.FromInt32(9090),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
			expected: `{
				  "/app-lifecycle/lifecycle-hooks-container/poststartz": {
					"httpGet": {
					  "path": "/startup",
					  "port": 9090,
					  "scheme": "HTTP"
					}
				  },
				  "/app-lifecycle/lifecycle-hooks-container/prestopz": {
					"httpGet": {
					  "path": "/shutdown",
					  "port": 8080,
					  "scheme": "HTTP"
					}
				  }
				}`,
		},
		{
			name: "preStop with TCP handler",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "prestop-tcp-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									TCPSocket: &corev1.TCPSocketAction{
										Port: intstr.FromInt32(8080),
									},
								},
							},
						},
					},
				},
			},
			expected: `{"/app-lifecycle/prestop-tcp-container/prestopz":{"tcpSocket":{"port":8080}}}`,
		},
		{
			name: "lifecycle hooks with excluded ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "excluded-lifecycle-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/shutdown",
										Port:   intstr.FromInt32(8080),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
			expected: "",
		},
		{
			name: "probes and lifecycle hooks on different ports",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "multi-handler-container",
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
								{ContainerPort: 7070},
								{ContainerPort: 6060},
							},
							LivenessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/health",
										Port: intstr.FromInt32(8080),
									},
								},
							},
							ReadinessProbe: &corev1.Probe{
								ProbeHandler: corev1.ProbeHandler{
									GRPC: &corev1.GRPCAction{
										Port: 9090,
									},
								},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/shutdown",
										Port:   intstr.FromInt32(7070),
										Scheme: "HTTP",
									},
								},
								PostStart: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path:   "/startup",
										Port:   intstr.FromInt32(6060),
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
			expected: `{
				"/app-health/multi-handler-container/livez": {
					"httpGet": {
						"path": "/health",
						"port": 8080
					}
				},
				"/app-health/multi-handler-container/readyz": {
					"grpc": {
						"port": 9090,
						"service": null
					}
				},
				"/app-lifecycle/multi-handler-container/poststartz": {
					"httpGet": {
						"path": "/startup",
						"port": 6060,
						"scheme": "HTTP"
					}
				},
				"/app-lifecycle/multi-handler-container/prestopz": {
					"httpGet": {
						"path": "/shutdown",
						"port": 7070,
						"scheme": "HTTP"
					}
				}
			}`,
		},
		{
			name: "lifecycle hooks with string port",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "string-port-container",
							Ports: []corev1.ContainerPort{
								{
									Name:          "http",
									ContainerPort: 8080,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							Lifecycle: &corev1.Lifecycle{
								PreStop: &corev1.LifecycleHandler{
									HTTPGet: &corev1.HTTPGetAction{
										Path: "/shutdown",
										Port: intstr.IntOrString{
											Type:   intstr.String,
											StrVal: "8080",
										},
										Scheme: "HTTP",
									},
								},
							},
						},
					},
				},
			},
			expected: `{
				"/app-lifecycle/string-port-container/prestopz": {
					"httpGet": {
						"path": "/shutdown",
						"port": 8080,
						"scheme": "HTTP"
					}
				}
			}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DumpAppProbers(tc.pod, targetPort)
			if len(tc.expected) > 0 {
				test.JSONEquals(t, got, tc.expected)
			} else {
				assert.Equal(t, 0, len(got))
			}

		})
	}
}

func TestGetIncludedPorts(t *testing.T) {
	tests := []struct {
		name     string
		pod      *corev1.Pod
		expected map[int32]bool
	}{
		{
			name: "nil annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
				},
			},
			// When no annotations are provided, expect an empty map (all ports included)
			expected: make(map[int32]bool),
		},
		{
			name: "empty annotations",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			// When annotations are empty, expect an empty map (all ports included)
			expected: make(map[int32]bool),
		},
		{
			name: "include all ports with asterisk",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "*",
					},
				},
			},
			// When includeInboundPorts is "*", expect an empty map (all ports included)
			expected: make(map[int32]bool),
		},
		{
			name: "specific include ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080,9090",
					},
				},
			},
			// When specific ports are included, expect a map with those ports
			expected: map[int32]bool{
				8080: true,
				9090: true,
			},
		},
		{
			name: "exclude ports not relevant when include specified",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080,9090",
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080,7070",
					},
				},
			},
			// When both include and exclude are specified, include takes precedence
			expected: map[int32]bool{
				8080: true,
				9090: true,
			},
		},
		{
			name: "only exclude ports specified",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080,7070",
					},
				},
			},
			// When only exclude is specified, expect a map without those ports
			expected: map[int32]bool{},
		},
		{
			name: "include with invalid port",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080,invalid,9090",
					},
				},
			},
			// Invalid ports should be ignored
			expected: map[int32]bool{
				8080: true,
				9090: true,
			},
		},
		{
			name: "exclude with invalid port",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080,invalid",
					},
				},
			},
			// Invalid exclude ports should be ignored
			expected: map[int32]bool{},
		},
		{
			name: "empty include ports string",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "",
					},
				},
			},
			// Empty include string is treated as not specified
			expected: map[int32]bool{},
		},
		{
			name: "empty exclude ports string",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "",
					},
				},
			},
			// Empty exclude string is treated as not specified
			expected: map[int32]bool{},
		},
		{
			name: "multiple include values with spaces",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080, 9090",
					},
				},
			},
			// Spaces in port list should be handled by splitPorts
			expected: map[int32]bool{
				8080: true,
				9090: true,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := getIncludedPorts(tc.pod)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("getIncludedPorts(%v) = %v, want %v", tc.pod, got, tc.expected)
			}
		})
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
