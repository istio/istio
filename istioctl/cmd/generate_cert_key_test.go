// Copyright 2019 Istio Authors.
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

package cmd

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

var (
	rootCrt, _          = ioutil.ReadFile("testdata/generatekeycert/ca.crt")
	rootkey, _          = ioutil.ReadFile("testdata/generatekeycert/ca.key")
	preStoredK8sConfig1 = []runtime.Object{
		&coreV1.Secret{
			ObjectMeta: metaV1.ObjectMeta{
				Name:      "istio-ca-secret",
				Namespace: "istio-system",
			},
			Data: map[string][]byte{
				"ca-cert.pem": rootCrt,
				"ca-key.pem":  rootkey},
		},

		&appsv1.DeploymentList{Items: []appsv1.Deployment{
			{
				ObjectMeta: metaV1.ObjectMeta{
					Name:      "istiod",
					Namespace: "istio-system",
				},
				Spec: appsv1.DeploymentSpec{
					Replicas: &one,
					Selector: &metaV1.LabelSelector{
						MatchLabels: map[string]string{"app": "istiod"},
					},
					Template: coreV1.PodTemplateSpec{
						ObjectMeta: metaV1.ObjectMeta{
							Labels: map[string]string{"app": "istiod"},
						},
						Spec: coreV1.PodSpec{
							Containers: []v1.Container{
								{Name: "details", Image: "gcr.io/pilot/latest",
									Env: []v1.EnvVar{{Name: "PILOT_CERT_PROVIDER", Value: "citadel"}}},
							},
						},
					},
				},
			},
		}},
	}
)

func TestGenerateKeyAndCert(t *testing.T) {
	cases := []testcase{
		{
			description:       "Invalid command args - missing workload name",
			args:              strings.Split("experimental generate-key-cert", " "),
			expectedException: true,
			expectedOutput:    "Error: workload name.namespace is required\n",
		},
		{
			description:       "Invalid command args - additional parameters",
			args:              strings.Split("experimental generate-key-cert nn.ns bb", " "),
			expectedException: true,
			expectedOutput:    "Error: only workload-name.vmnamespace is supported\n",
		},
		{
			description:       "valid case - generate key and cert",
			args:              strings.Split("experimental generate-key-cert vm.ns", " "),
			expectedException: false,
			k8sConfigs:        preStoredK8sConfig1,
			expectedOutput:    "root certificate, Certificate chain and private files successfully saved in \"root-cert.pem\",\"cert-chain.pem\" and \"key.pem\"\n",
			namespace:         "default",
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d %s", i, c.description), func(t *testing.T) {
			verifyGenerateKeyAndCert(t, c)
		})
	}
}

func verifyGenerateKeyAndCert(t *testing.T, c testcase) {
	t.Helper()

	interfaceFactory = mockInterfaceFactoryGenerator(c.k8sConfigs)
	var out bytes.Buffer
	rootCmd := GetRootCmd(c.args)
	rootCmd.SetOutput(&out)
	if c.namespace != "" {
		namespace = c.namespace
	}

	fErr := rootCmd.Execute()
	output := out.String()

	if c.expectedException {
		if fErr == nil {
			t.Fatalf("Wanted an exception, "+
				"didn't get one, output was %q", output)
		}
	} else {
		if fErr != nil {
			t.Fatalf("Unwanted exception: %v", fErr)
		}
	}

	if c.expectedOutput != "" && c.expectedOutput != output {
		t.Fatalf("Unexpected output for 'istioctl %s'\n got: %q\nwant: %q", strings.Join(c.args, " "), output, c.expectedOutput)
	}
}
