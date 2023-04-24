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
	"testing"

	"sigs.k8s.io/yaml"

	"istio.io/istio/cni/pkg/config"
	testutils "istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/test/util/assert"
)

const (
	k8sServiceHost = "10.96.0.1"
	k8sServicePort = "443"
	kubeCAFilepath = "testdata/kube-ca.crt"
)

func TestCreateKubeconfigFile(t *testing.T) {
	cases := []struct {
		name               string
		expectedFailure    bool
		k8sServiceProtocol string
		k8sServiceHost     string
		k8sServicePort     string
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
			name:           "skip TLS verify",
			k8sServiceHost: k8sServiceHost,
			k8sServicePort: k8sServicePort,
			skipTLSVerify:  true,
		},
		{
			name:           "TLS verify",
			k8sServiceHost: k8sServiceHost,
			k8sServicePort: k8sServicePort,
			kubeCAFilepath: kubeCAFilepath,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Create temp directory for files
			tempDir := t.TempDir()

			cfg := &config.InstallConfig{
				MountedCNINetDir:   tempDir,
				KubeCAFile:         c.kubeCAFilepath,
				K8sServiceProtocol: c.k8sServiceProtocol,
				K8sServiceHost:     c.k8sServiceHost,
				K8sServicePort:     c.k8sServicePort,
				SkipTLSVerify:      c.skipTLSVerify,
			}
			result, err := createKubeconfig(cfg)
			if err != nil {
				if !c.expectedFailure {
					t.Fatalf("did not expect failure: %v", err)
				}
				// Successful test case expecting failure
				return
			} else if c.expectedFailure {
				t.Fatalf("expected failure")
			}
			resultBytes, err := yaml.Marshal(result)
			assert.NoError(t, err)

			goldenFilepath := "testdata/kubeconfig-tls"
			if c.skipTLSVerify {
				goldenFilepath = "testdata/kubeconfig-skip-tls"
			}

			testutils.CompareContent(t, resultBytes, goldenFilepath)
		})
	}
}
