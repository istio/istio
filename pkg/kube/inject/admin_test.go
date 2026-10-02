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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pkg/envoy/admin"
)

func TestConfigureAdmin(t *testing.T) {
	for _, tt := range []struct {
		name, annotation, metadata   string
		native, shared, custom, fail bool
	}{
		{name: "default"},
		{name: "native TCP", native: true, annotation: "TCP"},
		{name: "native UDS", native: true, annotation: "UDS"},
		{name: "metadata UDS", native: true, metadata: "UDS"},
		{name: "annotation opt out", annotation: "TCP", metadata: "UDS"},
		{name: "annotation opt in", native: true, annotation: "UDS", metadata: "TCP"},
		{name: "traditional UDS", annotation: "UDS", fail: true},
		{name: "shared volume", native: true, annotation: "UDS", shared: true, fail: true},
		{name: "custom hooks", native: true, annotation: "UDS", custom: true, fail: true},
		{name: "invalid", native: true, annotation: "unknown", fail: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			original := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}}
			if tt.annotation != "" {
				original.Annotations[admin.Annotation] = tt.annotation
			}
			if tt.custom {
				original.Spec.Containers = append(original.Spec.Containers, corev1.Container{Name: ProxyContainerName, Lifecycle: &corev1.Lifecycle{}})
			}
			pod := original.DeepCopy()
			proxy := corev1.Container{Name: ProxyContainerName, Env: []corev1.EnvVar{{Name: admin.Env, Value: "TCP"}, {Name: admin.Env, Value: "UDS"}}, VolumeMounts: []corev1.VolumeMount{{Name: "istio-envoy", MountPath: "/etc/istio/proxy"}}}
			if tt.native {
				always := corev1.ContainerRestartPolicyAlways
				proxy.RestartPolicy = &always
				pod.Spec.InitContainers = []corev1.Container{proxy}
				pod.Spec.Containers = pod.Spec.Containers[:1]
			} else {
				pod.Spec.Containers = append(pod.Spec.Containers, proxy)
			}
			if tt.shared {
				pod.Spec.Containers[0].VolumeMounts = proxy.VolumeMounts
			}
			config := &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{}}
			if tt.metadata != "" {
				config.ProxyMetadata[admin.Env] = tt.metadata
			}
			values, err := NewValuesConfig("{}")
			if err != nil {
				t.Fatal(err)
			}
			err = configureAdmin(pod, InjectionParameters{pod: original, proxyConfig: config, valuesConfig: values})
			if (err != nil) != tt.fail {
				t.Fatalf("got %v, want failure %v", err, tt.fail)
			}
			if err != nil {
				return
			}
			proxy = *FindSidecar(pod)
			expected, _ := admin.Resolve(original.Annotations, config.ProxyMetadata)
			count := 0
			for _, e := range proxy.Env {
				if e.Name == admin.Env {
					count++
					if e.Value != string(expected) {
						t.Fatalf("transport %s", e.Value)
					}
				}
			}
			if count != 1 {
				t.Fatalf("found %d transport variables", count)
			}
			if expected == admin.UDS {
				want := []string{"pilot-agent", "request", "POST", "drain_listeners?inboundonly&graceful&skip_exit"}
				if !reflect.DeepEqual(proxy.Lifecycle.PreStop.Exec.Command, want) {
					t.Fatalf("unexpected hook: %v", proxy.Lifecycle)
				}
			}
		})
	}
}

func TestAdminUnsupportedOverrides(t *testing.T) {
	for _, name := range []string{"empty-metadata", "bootstrap-file", "bootstrap-template", "bootstrap-env", "custom-command", "template-arg", "template-selection", "bootstrap-annotation", "read-only", "subpath", "missing-volume", "init-mount", "ephemeral-mount", "rendered-hook"} {
		t.Run(name, func(t *testing.T) {
			always := corev1.ContainerRestartPolicyAlways
			pod := &corev1.Pod{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}, InitContainers: []corev1.Container{{Name: ProxyContainerName, RestartPolicy: &always, VolumeMounts: []corev1.VolumeMount{{Name: "istio-envoy", MountPath: "/etc/istio/proxy"}}}}}}
			original := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{}}}
			config := &meshconfig.ProxyConfig{ProxyMetadata: map[string]string{admin.Env: "UDS"}}
			values, err := NewValuesConfig("{}")
			if err != nil {
				t.Fatal(err)
			}
			proxy := FindSidecar(pod)
			switch name {
			case "empty-metadata":
				config.ProxyMetadata[admin.Env] = ""
			case "bootstrap-file":
				config.CustomConfigFile = "custom.json"
			case "bootstrap-template":
				config.ProxyBootstrapTemplatePath = "custom.tmpl"
			case "bootstrap-env":
				proxy.Env = []corev1.EnvVar{{Name: "ISTIO_BOOTSTRAP", Value: "custom.json"}}
			case "custom-command":
				proxy.Command = []string{"custom-proxy"}
			case "template-arg":
				proxy.Args = []string{"--templateFile=custom.json"}
			case "template-selection":
				original.Annotations["inject.istio.io/templates"] = "custom"
			case "bootstrap-annotation":
				original.Annotations["sidecar.istio.io/bootstrapOverride"] = "custom"
			case "read-only":
				proxy.VolumeMounts[0].ReadOnly = true
			case "subpath":
				proxy.VolumeMounts[0].SubPath = "shared"
			case "missing-volume":
				proxy.VolumeMounts = nil
			case "init-mount":
				pod.Spec.InitContainers = append(pod.Spec.InitContainers, corev1.Container{Name: "init-app", VolumeMounts: []corev1.VolumeMount{{Name: "istio-envoy", MountPath: "/shared"}}})
			case "ephemeral-mount":
				pod.Spec.EphemeralContainers = []corev1.EphemeralContainer{{EphemeralContainerCommon: corev1.EphemeralContainerCommon{Name: "debug", VolumeMounts: []corev1.VolumeMount{{Name: "istio-envoy", MountPath: "/shared"}}}}}
			case "rendered-hook":
				proxy.Lifecycle = &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"custom"}}}}
			}
			err = configureAdmin(pod, InjectionParameters{pod: original, proxyConfig: config, valuesConfig: values})
			if err == nil {
				t.Fatal("accepted unsupported override")
			}
		})
	}
}
