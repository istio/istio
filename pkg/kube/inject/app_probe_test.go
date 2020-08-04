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
	"testing"

	"istio.io/api/annotation"

	corev1 "k8s.io/api/core/v1"
)

func TestFindSidecar(t *testing.T) {
	proxy := corev1.Container{Name: "istio-proxy"}
	app := corev1.Container{Name: "app"}
	for _, tc := range []struct {
		name       string
		containers []corev1.Container
		index      int
	}{
		{"only-sidecar", []corev1.Container{proxy}, 0},
		{"app-and-sidecar", []corev1.Container{app, proxy}, 1},
		{"no-sidecar", []corev1.Container{app}, -1},
	} {
		got := FindSidecar(tc.containers)
		var want *corev1.Container
		if tc.index == -1 {
			want = nil
		} else {
			want = &tc.containers[tc.index]
		}
		if got != want {
			t.Errorf("[%v] failed, want %v, got %v", tc.name, want, got)
		}
	}
}

func TestShouldRewriteAppHTTPProbers(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		sidecarInjectionSpec SidecarInjectionSpec
		annotations          map[string]string
		expected             bool
	}{
		{
			name:                 "RewriteAppHTTPProbe-set-in-annotations",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: false},
			annotations:          nil,
			expected:             false,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-annotations",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: true},
			annotations:          nil,
			expected:             true,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-sidecar-injection-spec",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: false},
			annotations:          map[string]string{},
			expected:             false,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-sidecar-injection-spec",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: true},
			annotations:          map[string]string{},
			expected:             true,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-annotations",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: false},
			annotations:          map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "true"},
			expected:             true,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-sidecar-injection-spec-&-annotations",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: true},
			annotations:          map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "true"},
			expected:             true,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-annotations",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: false},
			annotations:          map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "false"},
			expected:             false,
		},
		{
			name:                 "RewriteAppHTTPProbe-set-in-sidecar-injection-spec-&-annotations",
			sidecarInjectionSpec: SidecarInjectionSpec{RewriteAppHTTPProbe: true},
			annotations:          map[string]string{annotation.SidecarRewriteAppHTTPProbers.Name: "false"},
			expected:             false,
		},
	} {
		got := ShouldRewriteAppHTTPProbers(tc.annotations, &tc.sidecarInjectionSpec)
		want := tc.expected
		if got != want {
			t.Errorf("[%v] failed, want %v, got %v", tc.name, want, got)
		}
	}
}
