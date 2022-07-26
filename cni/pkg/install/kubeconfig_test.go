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

package install

import (
	"os"
	"path/filepath"
	"testing"

	"istio.io/istio/cni/pkg/config"
	"istio.io/istio/cni/pkg/constants"
	testutils "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/file"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	k8sServiceHost = "10.96.0.1"
	k8sServicePort = "443"
	kubeCAFilepath = "testdata/kube-ca.crt"
	saToken        = "service_account_token_string"
)

func TestCreateKubeconfigFile(t *testing.T) {
	cases := []struct {
		name               string
		expectedFailure    bool
		kubeconfigFilename string
		kubeconfigMode     int
		k8sServiceProtocol string
		k8sServiceHost     string
		k8sServicePort     string
		saToken            string
		kubeCAFilepath     string
		skipTLSVerify      bool
	}{
		{
			name:            "k8s service host not set",
			expectedFailure: true,
		},
		{
			name:            "k8s service port not set",
			expectedFailure: true,
			k8sServiceHost:  k8sServiceHost,
		},
		{
			name:               "skip TLS verify",
			kubeconfigFilename: "istio-cni-kubeconfig",
			kubeconfigMode:     constants.DefaultKubeconfigMode,
			k8sServiceHost:     k8sServiceHost,
			k8sServicePort:     k8sServicePort,
			saToken:            saToken,
			skipTLSVerify:      true,
		},
		{
			name:               "TLS verify",
			kubeconfigFilename: "istio-cni-kubeconfig",
			kubeconfigMode:     constants.DefaultKubeconfigMode,
			k8sServiceHost:     k8sServiceHost,
			k8sServicePort:     k8sServicePort,
			saToken:            saToken,
			kubeCAFilepath:     kubeCAFilepath,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			tempDir := t.TempDir()

			cfg := &config.InstallConfig{
				MountedCNINetDir:   tempDir,
				KubeconfigFilename: c.kubeconfigFilename,
				KubeconfigMode:     c.kubeconfigMode,
				KubeCAFile:         c.kubeCAFilepath,
				K8sServiceProtocol: c.k8sServiceProtocol,
				K8sServiceHost:     c.k8sServiceHost,
				K8sServicePort:     c.k8sServicePort,
				SkipTLSVerify:      c.skipTLSVerify,
			}
			resultFilepath, err := createKubeconfigFile(cfg, c.saToken)
			if err != nil {
				assert.Equal(t, resultFilepath, "")
				if !c.expectedFailure {
					t.Fatalf("did not expect failure: %v", err)
				}
				// Successful test case expecting failure
				return
			} else if c.expectedFailure {
				t.Fatalf("expected failure")
			}

			if len(resultFilepath) == 0 {
				t.Fatalf("received empty filepath result, expected a kubeconfig filepath")
			}

			expectedFilepath := filepath.Join(tempDir, c.kubeconfigFilename)
			if resultFilepath != expectedFilepath {
				t.Fatalf("expected %s, got %s", expectedFilepath, resultFilepath)
			}

			if !file.Exists(resultFilepath) {
				t.Fatalf("kubeconfig file does not exist: %s", resultFilepath)
			}

			info, err := os.Stat(resultFilepath)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode() != os.FileMode(c.kubeconfigMode) {
				t.Fatalf("kubeconfig file mode incorrectly set: expected: %#o, got: %#o", os.FileMode(c.kubeconfigMode), info.Mode())
			}

			var goldenFilepath string
			if c.skipTLSVerify {
				goldenFilepath = "testdata/kubeconfig-skip-tls"
			} else {
				goldenFilepath = "testdata/kubeconfig-tls"
			}

			goldenConfig := testutils.ReadFile(t, goldenFilepath)
			resultConfig := testutils.ReadFile(t, resultFilepath)
			testutils.CompareBytes(t, resultConfig, goldenConfig, goldenFilepath)
		})
	}
}
