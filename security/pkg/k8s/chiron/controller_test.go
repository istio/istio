// Copyright 2019 Istio Authors
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

package chiron

import (
	"testing"
	"time"

	"k8s.io/client-go/kubernetes/fake"
)

func TestNewWebhookController(t *testing.T) {
	client := fake.NewSimpleClientset()
	mutatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}
	validatingWebhookConfigFiles := []string{"./test-data/empty-webhook-config.yaml"}

	testCases := map[string]struct {
		deleteWebhookConfigOnExit     bool
		gracePeriodRatio              float32
		minGracePeriod                time.Duration
		k8sCaCertFile                 string
		namespace                     string
		mutatingWebhookConfigFiles    []string
		mutatingWebhookConfigNames    []string
		mutatingWebhookServiceNames   []string
		mutatingWebhookServicePorts   []int
		validatingWebhookConfigFiles  []string
		validatingWebhookConfigNames  []string
		validatingWebhookServiceNames []string
		validatingWebhookServicePorts []int
		shouldFail                    bool
	}{
		"invalid grade period ratio": {
			gracePeriodRatio:             1.5,
			k8sCaCertFile:                "./test-data/example-invalid-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   true,
		},
		"invalid CA cert path": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./invalid-path/invalid-file",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   true,
		},
		"valid CA cert path": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   false,
		},
		"invalid mutating webhook config file": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   []string{"./invalid-path/invalid-file"},
			validatingWebhookConfigFiles: validatingWebhookConfigFiles,
			shouldFail:                   true,
		},
		"invalid validatating webhook config file": {
			gracePeriodRatio:             0.6,
			k8sCaCertFile:                "./test-data/example-ca-cert.pem",
			mutatingWebhookConfigFiles:   mutatingWebhookConfigFiles,
			validatingWebhookConfigFiles: []string{"./invalid-path/invalid-file"},
			shouldFail:                   true,
		},
	}

	for _, tc := range testCases {
		_, err := NewWebhookController(tc.deleteWebhookConfigOnExit, tc.gracePeriodRatio, tc.minGracePeriod,
			client.CoreV1(), client.AdmissionregistrationV1beta1(), client.CertificatesV1beta1(),
			tc.k8sCaCertFile, tc.namespace, tc.mutatingWebhookConfigFiles, tc.mutatingWebhookConfigNames,
			tc.mutatingWebhookServiceNames, tc.mutatingWebhookServicePorts, tc.validatingWebhookConfigFiles,
			tc.validatingWebhookConfigNames, tc.validatingWebhookServiceNames, tc.validatingWebhookServicePorts)
		if tc.shouldFail {
			if err == nil {
				t.Errorf("should have failed at NewWebhookController()")
			} else {
				// Should fail, skip the current case.
				continue
			}
		} else if err != nil {
			t.Errorf("failed at NewWebhookController(): %v", err)
		}
	}
}
