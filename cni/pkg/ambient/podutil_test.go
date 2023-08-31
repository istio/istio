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

package ambient

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"istio.io/api/annotation"
	"istio.io/istio/pkg/config/constants"
)

func Test_getEnvFromPod(t *testing.T) {
	type args struct {
		pod     *corev1.Pod
		envName string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "test empty env",
			args: args{
				pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Env: []corev1.EnvVar{},
							},
						},
					},
				},
				envName: "test",
			},
			want: "",
		},
		{
			name: "test get env from pod",
			args: args{
				pod: &corev1.Pod{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Env: []corev1.EnvVar{
									{
										Name:  "TEST_ENV",
										Value: "TEST_VALUE",
									},
								},
							},
						},
					},
				},
			},
			want: "TEST_VALUE",
		},
		{
			name: "test if pod is nil",
			args: args{
				pod:     nil,
				envName: "test",
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getEnvFromPod(tt.args.pod, tt.args.envName); got != tt.want {
				t.Errorf("getEnvFromPod() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPodRedirectionEnabled(t *testing.T) {
	type args struct {
		namespace *corev1.Namespace
		pod       *corev1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "test does not have ambient mode enabled",
			args: args{
				namespace: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							constants.DataplaneMode: "unknown",
						},
					},
				},
				pod: nil,
			},
			want: false,
		},
		{
			name: "test pod has ztunnel label",
			args: args{
				namespace: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							constants.DataplaneMode: constants.DataplaneModeAmbient,
						},
					},
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotation.SidecarStatus.Name: "test",
						},
					},
				},
			},
			want: false,
		},
		{
			name: "test pod redirection is enabled",
			args: args{
				namespace: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							constants.DataplaneMode: constants.DataplaneModeAmbient,
						},
					},
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							constants.AmbientRedirection: constants.AmbientRedirectionEnabled,
						},
					},
				},
			},
			want: true,
		},
		{
			name: "test pod redirection is disabled",
			args: args{
				namespace: &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							constants.DataplaneMode: constants.DataplaneModeAmbient,
						},
					},
				},
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := PodRedirectionEnabled(tt.args.namespace, tt.args.pod); got != tt.want {
				t.Errorf("PodRedirectionEnabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_podHasSidecar(t *testing.T) {
	type args struct {
		pod *corev1.Pod
	}
	tests := []struct {
		name string
		args args
		want bool
	}{
		{
			name: "test pod has sidecar",
			args: args{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{
							annotation.SidecarStatus.Name: "test",
						},
					},
				},
			},
			want: true,
		},
		{
			name: "test pod does not have sidecar",
			args: args{
				pod: &corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
			want: false,
		},
		{
			name: "test pod is nil",
			args: args{
				pod: nil,
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := podHasSidecar(tt.args.pod); got != tt.want {
				t.Errorf("podHasSidecar() = %v, want %v", got, tt.want)
			}
		})
	}
}
