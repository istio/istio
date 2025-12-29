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

package kube

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/ptr"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/util/sets"
)

func TestBuildClientConfig(t *testing.T) {
	config1 := generateKubeConfig(t, "1.1.1.1", "3.3.3.3")
	config2 := generateKubeConfig(t, "2.2.2.2", "4.4.4.4")

	tests := []struct {
		name               string
		explicitKubeconfig string
		envKubeconfig      string
		context            string
		wantErr            bool
		wantHost           string
	}{
		{
			name:               "DefaultSystemKubeconfig",
			explicitKubeconfig: "",
			envKubeconfig:      config1,
			wantErr:            false,
			wantHost:           "https://1.1.1.1:8001",
		},
		{
			name:               "SinglePath",
			explicitKubeconfig: config1,
			wantErr:            false,
			envKubeconfig:      "",
			wantHost:           "https://1.1.1.1:8001",
		},
		{
			name:               "MultiplePathsFirst",
			explicitKubeconfig: "",
			wantErr:            false,
			envKubeconfig:      fmt.Sprintf("%s:%s", config1, config2),
			wantHost:           "https://1.1.1.1:8001",
		},
		{
			name:               "MultiplePathsSecond",
			explicitKubeconfig: "",
			wantErr:            false,
			envKubeconfig:      fmt.Sprintf("missing:%s", config2),
			wantHost:           "https://2.2.2.2:8001",
		},
		{
			name:               "NonCurrentContext",
			explicitKubeconfig: config1,
			wantErr:            false,
			envKubeconfig:      "",
			context:            "cluster2.local-context",
			wantHost:           "https://3.3.3.3:8001",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("KUBECONFIG", tt.envKubeconfig)

			resp, err := BuildClientConfig(tt.explicitKubeconfig, tt.context)
			if (err != nil) != tt.wantErr {
				t.Fatalf("BuildClientConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if resp != nil && resp.Host != tt.wantHost {
				t.Fatalf("Incorrect host. Got: %s, Want: %s", resp.Host, tt.wantHost)
			}
		})
	}
}

func generateKubeConfig(t *testing.T, cluster1Host string, cluster2Host string) string {
	t.Helper()

	tempDir := t.TempDir()
	filePath := filepath.Join(tempDir, "config")

	template := `apiVersion: v1
kind: Config
clusters:
- cluster:
    insecure-skip-tls-verify: true
    server: https://%s:8001
  name: cluster.local
- cluster:
    insecure-skip-tls-verify: true
    server: https://%s:8001
  name: cluster2.local
contexts:
- context:
    cluster: cluster.local
    namespace: default
    user: admin
  name: cluster.local-context
- context:
    cluster: cluster2.local
    namespace: default
    user: admin
  name: cluster2.local-context
current-context: cluster.local-context
preferences: {}
users:
- name: admin
  user:
    token: sdsddsd`

	sampleConfig := fmt.Sprintf(template, cluster1Host, cluster2Host)
	err := os.WriteFile(filePath, []byte(sampleConfig), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	return filePath
}

func TestCronJobMetadata(t *testing.T) {
	tests := []struct {
		name             string
		jobName          string
		wantTypeMetadata metav1.TypeMeta
		wantName         types.NamespacedName
	}{
		{
			name:    "cron-job-name-sec",
			jobName: "sec-1234567890",
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "CronJob",
				APIVersion: "batch/v1",
			},
			wantName: types.NamespacedName{
				Name: "sec",
			},
		},
		{
			name:    "cron-job-name-min",
			jobName: "min-12345678",
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "CronJob",
				APIVersion: "batch/v1",
			},
			wantName: types.NamespacedName{
				Name: "min",
			},
		},
		{
			name:    "non-cron-job-name",
			jobName: "job-123",
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "Job",
				APIVersion: "v1",
			},
			wantName: types.NamespacedName{
				Name: "job-123",
			},
		},
	}

	for _, tt := range tests {
		controller := true
		t.Run(tt.name, func(t *testing.T) {
			gotObjectMeta, gotTypeMeta := GetDeployMetaFromPod(
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						GenerateName: tt.jobName + "-pod",
						OwnerReferences: []metav1.OwnerReference{{
							APIVersion: "v1",
							Controller: &controller,
							Kind:       "Job",
							Name:       tt.jobName,
						}},
					},
				},
			)
			if !reflect.DeepEqual(gotObjectMeta, tt.wantName) {
				t.Errorf("Object metadata got %+v want %+v", gotObjectMeta, tt.wantName)
			}
			if !reflect.DeepEqual(gotTypeMeta, tt.wantTypeMetadata) {
				t.Errorf("Type metadata got %+v want %+v", gotTypeMeta, tt.wantTypeMetadata)
			}
		})
	}
}

func TestDeployMeta(t *testing.T) {
	tests := []struct {
		name             string
		pod              *corev1.Pod
		wantTypeMetadata metav1.TypeMeta
		wantName         types.NamespacedName
	}{
		{
			name: "deployconfig-name-deploy",
			pod:  podForDeploymentConfig("deploy", true),
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "DeploymentConfig",
				APIVersion: "v1",
			},
			wantName: types.NamespacedName{
				Name: "deploy",
			},
		},
		{
			name: "deployconfig-name-deploy2",
			pod:  podForDeploymentConfig("deploy2", true),
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "DeploymentConfig",
				APIVersion: "v1",
			},
			wantName: types.NamespacedName{
				Name: "deploy2",
			},
		},
		{
			name: "non-deployconfig-label",
			pod:  podForDeploymentConfig("dep", false),
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "ReplicationController",
				APIVersion: "v1",
			},
			wantName: types.NamespacedName{
				Name: "dep-rc",
			},
		},
		{
			name: "argo-rollout",
			pod: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "name-6dc78b855c-",
					OwnerReferences: []metav1.OwnerReference{{
						APIVersion: "v1",
						Controller: ptr.Of(true),
						Kind:       "ReplicaSet",
						Name:       "name-6dc78b855c",
					}},
					Labels: map[string]string{
						"rollouts-pod-template-hash": "6dc78b855c",
					},
				},
			},
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "Rollout",
				APIVersion: "v1alpha1",
			},
			wantName: types.NamespacedName{
				Name: "name",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotTypeMeta := GetDeployMetaFromPod(tt.pod)
			assert.Equal(t, gotName, tt.wantName)
			assert.Equal(t, gotTypeMeta, tt.wantTypeMetadata)
		})
	}
}

func podForDeploymentConfig(deployConfigName string, hasDeployConfigLabel bool) *corev1.Pod {
	controller := true
	labels := make(map[string]string)
	if hasDeployConfigLabel {
		labels["deploymentconfig"] = deployConfigName
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: deployConfigName + "-rc-pod",
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "v1",
				Controller: &controller,
				Kind:       "ReplicationController",
				Name:       deployConfigName + "-rc",
			}},
			Labels: labels,
		},
	}
}

func TestSanitizeKubeConfig(t *testing.T) {
	cases := []struct {
		name      string
		config    api.Config
		allowlist sets.String
		want      api.Config
		wantErr   bool
	}{
		{
			name:    "empty",
			config:  api.Config{},
			want:    api.Config{},
			wantErr: false,
		},
		{
			name: "exec",
			config: api.Config{
				AuthInfos: map[string]*api.AuthInfo{
					"default": {
						Exec: &api.ExecConfig{
							Command: "sleep",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name:      "exec allowlist",
			allowlist: sets.New("exec"),
			config: api.Config{
				AuthInfos: map[string]*api.AuthInfo{
					"default": {
						Exec: &api.ExecConfig{
							Command: "sleep",
						},
					},
				},
			},
			want: api.Config{
				AuthInfos: map[string]*api.AuthInfo{
					"default": {
						Exec: &api.ExecConfig{
							Command: "sleep",
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := sanitizeKubeConfig(tt.config, tt.allowlist)
			if (err != nil) != tt.wantErr {
				t.Fatalf("sanitizeKubeConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tt.config, tt.want); diff != "" {
				t.Fatal(diff)
			}
		})
	}
}
