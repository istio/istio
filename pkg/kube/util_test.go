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
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
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
	tempDir, err := ioutil.TempDir("/tmp/", ".kube")
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
	err = ioutil.WriteFile(filePath, []byte(sampleConfig), 0644)
	if err != nil {
		return "", err
	}
	return filePath, nil
}
