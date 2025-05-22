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
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
		name          string
		pod           *corev1.Pod
		expected      string
		errorExpected bool
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
			expected:      `{}`,
			errorExpected: true,
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
			expected:      `{"/app-health/foo/startupz":{"httpGet":{"path":"/health/full","port":8080,"host":"localhost","scheme":"http"},"timeoutSeconds":10}}`,
			errorExpected: false,
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
			expected: `{"/app-health/multi-probe-container/livez":{"httpGet":{"path":"/health","port":8080}},"/app-health/multi-probe-container/readyz":{"grpc":{"port":9090,"service":null}},"/app-health/multi-probe-container/startupz":{"tcpSocket":{"port":7070}}}`,
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
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
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
			expected:      `{}`,
			errorExpected: true,
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
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
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
			expected:      `{"/app-health/excluded-port-container/readyz":{"httpGet":{"path":"/health","port":7070}}}`,
			errorExpected: false,
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
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
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
			expected: `{"/app-lifecycle/poststart-http-container/poststartz":{"httpGet":{"path":"/startup","port":8080,"scheme":"HTTP"}}}`,
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
			expected: `{"/app-lifecycle/lifecycle-hooks-container/poststartz":{"httpGet":{"path":"/startup","port":9090,"scheme":"HTTP"}},"/app-lifecycle/lifecycle-hooks-container/prestopz":{"httpGet":{"path":"/shutdown","port":8080,"scheme":"HTTP"}}}`,
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
			expected:      `{}`,
			errorExpected: true,
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
			expected: `{"/app-health/multi-handler-container/livez":{"httpGet":{"path":"/health","port":8080}},"/app-health/multi-handler-container/readyz":{"grpc":{"port":9090,"service":null}},"/app-lifecycle/multi-handler-container/poststartz":{"httpGet":{"path":"/startup","port":6060,"scheme":"HTTP"}},"/app-lifecycle/multi-handler-container/prestopz":{"httpGet":{"path":"/shutdown","port":7070,"scheme":"HTTP"}}}`,
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
			expected: `{"/app-lifecycle/string-port-container/prestopz":{"httpGet":{"path":"/shutdown","port":8080,"scheme":"HTTP"}}}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DumpAppProbers(tc.pod, targetPort)

			if tc.errorExpected {
				var data map[string]interface{}
				if err := json.Unmarshal([]byte(got), &data); err == nil {
					t.Fatal("Expected error unmarshaling invalid JSON, but got nil")
				}
			} else {
				test.JSONEquals(t, got, tc.expected)
			}
		})
	}
}

func TestGetIncludedPorts(t *testing.T) {
	tests := []struct {
		name string
		pod  *corev1.Pod
		want map[int32]bool
	}{
		{
			name: "nil annotations - should include all ports",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true, 9090: true},
		},
		{
			name: "empty annotations - should include all ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true, 9090: true},
		},
		{
			name: "includeInboundPorts with * - should include all ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "*",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true, 9090: true},
		},
		{
			name: "includeInboundPorts with specific ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080,9091",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true, 9091: true},
		},
		{
			name: "includeInboundPorts with invalid port",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080,invalid",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true},
		},
		{
			name: "excludeInboundPorts with specific ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "9090",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true},
		},
		{
			name: "excludeInboundPorts with invalid port",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficExcludeInboundPorts.Name: "9090,invalid",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true},
		},
		{
			name: "both include and exclude annotations - include takes precedence",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "8080",
						annotation.SidecarTrafficExcludeInboundPorts.Name: "8080", // This should be ignored
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true},
		},
		{
			name: "empty includeInboundPorts - should include all ports",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						annotation.SidecarTrafficIncludeInboundPorts.Name: "",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true, 9090: true},
		},
		{
			name: "multiple containers with ports",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 8080},
							},
						},
						{
							Ports: []corev1.ContainerPort{
								{ContainerPort: 9090},
							},
						},
					},
				},
			},
			want: map[int32]bool{8080: true, 9090: true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := getIncludedPorts(tt.pod)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getIncludedPorts() = %v, want %v", got, tt.want)
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
