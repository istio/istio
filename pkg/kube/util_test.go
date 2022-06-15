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

	kubeApiCore "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	networkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
)

func TestBuildClientConfig(t *testing.T) {
	config1, err := generateKubeConfig("1.1.1.1", "3.3.3.3")
	if err != nil {
		t.Fatalf("Failed to create a sample kubernetes config file. Err: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(config1))
	config2, err := generateKubeConfig("2.2.2.2", "4.4.4.4")
	if err != nil {
		t.Fatalf("Failed to create a sample kubernetes config file. Err: %v", err)
	}
	defer os.RemoveAll(filepath.Dir(config2))

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
			currentEnv := os.Getenv("KUBECONFIG")
			err := os.Setenv("KUBECONFIG", tt.envKubeconfig)
			if err != nil {
				t.Fatalf("Failed to set KUBECONFIG environment variable")
			}
			defer os.Setenv("KUBECONFIG", currentEnv)

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

func generateKubeConfig(cluster1Host string, cluster2Host string) (string, error) {
	tempDir, err := os.MkdirTemp("/tmp/", ".kube")
	if err != nil {
		return "", err
	}
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
	err = os.WriteFile(filePath, []byte(sampleConfig), 0o644)
	if err != nil {
		return "", err
	}
	return filePath, nil
}

func TestCronJobMetadata(t *testing.T) {
	tests := []struct {
		name               string
		jobName            string
		wantTypeMetadata   metav1.TypeMeta
		wantObjectMetadata metav1.ObjectMeta
	}{
		{
			name:    "cron-job-name-sec",
			jobName: "sec-1234567890",
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "CronJob",
				APIVersion: "batch/v1beta1",
			},
			wantObjectMetadata: metav1.ObjectMeta{
				Name:         "sec",
				GenerateName: "sec-1234567890-pod",
			},
		},
		{
			name:    "cron-job-name-min",
			jobName: "min-12345678",
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "CronJob",
				APIVersion: "batch/v1beta1",
			},
			wantObjectMetadata: metav1.ObjectMeta{
				Name:         "min",
				GenerateName: "min-12345678-pod",
			},
		},
		{
			name:    "non-cron-job-name",
			jobName: "job-123",
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "Job",
				APIVersion: "v1",
			},
			wantObjectMetadata: metav1.ObjectMeta{
				Name:         "job-123",
				GenerateName: "job-123-pod",
			},
		},
	}

	for _, tt := range tests {
		controller := true
		t.Run(tt.name, func(t *testing.T) {
			gotObjectMeta, gotTypeMeta := GetDeployMetaFromPod(
				&kubeApiCore.Pod{
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
			if !reflect.DeepEqual(gotObjectMeta, tt.wantObjectMetadata) {
				t.Errorf("Object metadata got %+v want %+v", gotObjectMeta, tt.wantObjectMetadata)
			}
			if !reflect.DeepEqual(gotTypeMeta, tt.wantTypeMetadata) {
				t.Errorf("Type metadata got %+v want %+v", gotTypeMeta, tt.wantTypeMetadata)
			}
		})
	}
}

func TestDeploymentConfigMetadata(t *testing.T) {
	tests := []struct {
		name               string
		pod                *kubeApiCore.Pod
		wantTypeMetadata   metav1.TypeMeta
		wantObjectMetadata metav1.ObjectMeta
	}{
		{
			name: "deployconfig-name-deploy",
			pod:  podForDeploymentConfig("deploy", true),
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "DeploymentConfig",
				APIVersion: "v1",
			},
			wantObjectMetadata: metav1.ObjectMeta{
				Name:         "deploy",
				GenerateName: "deploy-rc-pod",
				Labels:       map[string]string{},
			},
		},
		{
			name: "deployconfig-name-deploy2",
			pod:  podForDeploymentConfig("deploy2", true),
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "DeploymentConfig",
				APIVersion: "v1",
			},
			wantObjectMetadata: metav1.ObjectMeta{
				Name:         "deploy2",
				GenerateName: "deploy2-rc-pod",
				Labels:       map[string]string{},
			},
		},
		{
			name: "non-deployconfig-label",
			pod:  podForDeploymentConfig("dep", false),
			wantTypeMetadata: metav1.TypeMeta{
				Kind:       "ReplicationController",
				APIVersion: "v1",
			},
			wantObjectMetadata: metav1.ObjectMeta{
				Name:         "dep-rc",
				GenerateName: "dep-rc-pod",
				Labels:       map[string]string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotObjectMeta, gotTypeMeta := GetDeployMetaFromPod(tt.pod)
			if !reflect.DeepEqual(gotObjectMeta, tt.wantObjectMetadata) {
				t.Errorf("Object metadata got %+v want %+v", gotObjectMeta, tt.wantObjectMetadata)
			}
			if !reflect.DeepEqual(gotTypeMeta, tt.wantTypeMetadata) {
				t.Errorf("Type metadata got %+v want %+v", gotTypeMeta, tt.wantTypeMetadata)
			}
		})
	}
}

func podForDeploymentConfig(deployConfigName string, hasDeployConfigLabel bool) *kubeApiCore.Pod {
	controller := true
	labels := make(map[string]string)
	if hasDeployConfigLabel {
		labels["deploymentconfig"] = deployConfigName
	}
	return &kubeApiCore.Pod{
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

func TestStripUnusedFields(t *testing.T) {
	tests := []struct {
		name string
		obj  interface{}
		want interface{}
	}{
		{
			name: "transform pods",
			obj: &kubeApiCore.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Labels:      map[string]string{"a": "b"},
					Annotations: map[string]string{"c": "d"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager: "whatever",
						},
					},
				},
			},
			want: &kubeApiCore.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Labels:      map[string]string{"a": "b"},
					Annotations: map[string]string{"c": "d"},
				},
			},
		},
		{
			name: "transform endpoints",
			obj: &kubeApiCore.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Labels:      map[string]string{"a": "b"},
					Annotations: map[string]string{"c": "d"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager: "whatever",
						},
					},
				},
			},
			want: &kubeApiCore.Endpoints{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Labels:      map[string]string{"a": "b"},
					Annotations: map[string]string{"c": "d"},
				},
			},
		},
		{
			name: "transform virtual services",
			obj: &networkingv1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Labels:      map[string]string{"a": "b"},
					Annotations: map[string]string{"c": "d"},
					ManagedFields: []metav1.ManagedFieldsEntry{
						{
							Manager: "whatever",
						},
					},
				},
			},
			want: &networkingv1alpha3.VirtualService{
				ObjectMeta: metav1.ObjectMeta{
					Namespace:   "foo",
					Name:        "bar",
					Labels:      map[string]string{"a": "b"},
					Annotations: map[string]string{"c": "d"},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, _ := StripUnusedFields(tt.obj)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("StripUnusedFields: got %v, want %v", got, tt.want)
			}
		})
	}
}
