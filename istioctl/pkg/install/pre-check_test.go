// Copyright Istio Authors.
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
	"bytes"
	"fmt"
	"testing"

	authorizationapi "k8s.io/api/authorization/v1beta1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

type mockClientExecPreCheckConfig struct {
	namespace  string
	version    *version.Info
	authConfig *authorizationapi.SelfSubjectAccessReview
}
type testcase struct {
	description       string
	config            *mockClientExecPreCheckConfig
	expectedException bool
}

var (
	version1_16 = &version.Info{
		Major:      "1",
		Minor:      "16",
		GitVersion: "1.16",
	}
	version1_8 = &version.Info{
		Major:      "1",
		Minor:      "8",
		GitVersion: "1.8",
	}
	version1_16GKE = &version.Info{
		Major:      "1",
		Minor:      "16+",
		GitVersion: "v1.16.7-gke.10",
	}
	version1_8GKE = &version.Info{
		Major:      "1",
		Minor:      "8",
		GitVersion: "v1.8.7-gke.8",
	}
	versionInvalid = &version.Info{
		Major:      "1",
		Minor:      "8",
		GitVersion: "v1.invalid.7",
	}
)

func TestPreCheck(t *testing.T) {
	cases := []testcase{
		{
			description: "Lower Kubernetes Version",
			config: &mockClientExecPreCheckConfig{
				version:   version1_8,
				namespace: "test",
			},
			expectedException: true,
		},
		{
			description: "Invalid Kubernetes Version",
			config: &mockClientExecPreCheckConfig{
				version:   versionInvalid,
				namespace: "test",
			},
			expectedException: true,
		},
		{
			description: "Valid Kubernetes Version against GKE",
			config: &mockClientExecPreCheckConfig{
				version:   version1_16GKE,
				namespace: "test",
			},
			expectedException: false,
		},
		{
			description: "Invalid Kubernetes Version against GKE",
			config: &mockClientExecPreCheckConfig{
				version:   version1_8GKE,
				namespace: "test",
			},
			expectedException: true,
		},
		{description: "Invalid Istio System",
			config: &mockClientExecPreCheckConfig{
				version:   version1_16,
				namespace: "istio-system",
			},
			expectedException: false, // It is fine to precheck an existing namespace; we might be installing canary control plane
		},
		{description: "Valid Istio System",
			config: &mockClientExecPreCheckConfig{
				version:   version1_16,
				namespace: "test",
			},
			expectedException: false,
		},
		{description: "Lacking Permission",
			config: &mockClientExecPreCheckConfig{
				version:   version1_16,
				namespace: "test",
				authConfig: &authorizationapi.SelfSubjectAccessReview{
					Spec: authorizationapi.SelfSubjectAccessReviewSpec{
						ResourceAttributes: &authorizationapi.ResourceAttributes{
							Namespace: "test",
							Verb:      "create",
							Group:     "test",
							Version:   "test",
							Resource:  "test",
						},
					},
				},
			},
			expectedException: true,
		},
		{description: "Valid Case",
			config: &mockClientExecPreCheckConfig{
				version:   version1_16,
				namespace: "test",
			},
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyOutput(t, c)
		})
	}
}

func verifyOutput(t *testing.T, c testcase) {
	t.Helper()

	clientFactory = mockPreCheckClient(c.config)
	var out bytes.Buffer
	precheckCmd := NewPrecheckCommand()
	precheckCmd.SetOut(&out)
	precheckCmd.SetErr(&out)
	fErr := precheckCmd.Execute()
	output := out.String()
	if c.expectedException {
		if fErr == nil {
			t.Fatalf("Wanted an exception for 'istioctl x precheck',"+
				"didn't get one, output was %q", output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception for 'istioctl x precheck': %v", fErr)
		}
	}
}

func mockPreCheckClient(m *mockClientExecPreCheckConfig) func(restClientGetter genericclioptions.RESTClientGetter) (preCheckExecClient, error) {
	outfunction := func(restClientGetter genericclioptions.RESTClientGetter) (preCheckExecClient, error) {
		return m, nil
	}
	return outfunction
}

// nolint: unparam
func (m *mockClientExecPreCheckConfig) serverVersion() (*version.Info, error) {
	return m.version, nil
}

// nolint: unparam
func (m *mockClientExecPreCheckConfig) getNameSpace(ns string) (*v1.Namespace, error) {
	if m.namespace == ns {
		n := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: m.namespace,
			},
		}
		return n, nil
	}
	return nil, fmt.Errorf("namespaces \"%s\" not found", ns)

}

func (m *mockClientExecPreCheckConfig) checkAuthorization(
	s *authorizationapi.SelfSubjectAccessReview) (result *authorizationapi.SelfSubjectAccessReview, err error) {
	if m.authConfig != nil {
		return m.authConfig, nil
	}
	authConfig := &authorizationapi.SelfSubjectAccessReview{
		Spec: authorizationapi.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationapi.ResourceAttributes{
				Namespace: "test",
				Verb:      "create",
				Group:     "test",
				Version:   "test",
				Resource:  "test",
			},
		},
		Status: authorizationapi.SubjectAccessReviewStatus{
			Allowed: true,
		},
	}
	return authConfig, nil

}

func (m *mockClientExecPreCheckConfig) checkMutatingWebhook() error {
	return nil
}

func (m *mockClientExecPreCheckConfig) getIstioInstalls() ([]istioInstall, error) {
	return []istioInstall{}, nil
}
