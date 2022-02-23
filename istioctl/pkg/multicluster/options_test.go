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

package multicluster

import (
	"github.com/spf13/pflag"
	"istio.io/istio/pkg/test/util/assert"
	"testing"
)

type KubeVals struct {
	KubeConfigValue string
	ContextValue string
	NamespaceValue string
}

func TestPrepare(t *testing.T) {
	cases := []struct {
		name     string
		KubeOpts *KubeOptions
		KubeOptsValue *KubeVals
		expected *KubeVals
	}{
		{
			"test1",
			&KubeOptions{"kubeconfig", "context", "namespace"},
			&KubeVals{"sample kube config", "sample context", "sample namespace"},
			&KubeVals{"sample kube config", "sample context", "sample namespace"},
		},
		{
			"test2",
			&KubeOptions{"", "context", "namespace"},
			&KubeVals{"", "sample context", "sample namespace"},
			&KubeVals{"", "sample context", "sample namespace"},
		},
		{
			"test3",
			&KubeOptions{"kubeconfig", "", "namespace"},
			&KubeVals{"sample kube config", "", "sample namespace"},
			&KubeVals{"sample kube config", "", "sample namespace"},
		},
		{
			"test4",
			&KubeOptions{"kubeconfig", "context", ""},
			&KubeVals{"sample kube config", "sample context", ""},
			&KubeVals{"sample kube config", "sample context", defaultIstioNamespace},
		},
		{
			"test5",
			&KubeOptions{"", "", ""},
			&KubeVals{"", "", ""},
			&KubeVals{"", "", defaultIstioNamespace},
		},
		{
			"test6",
			&KubeOptions{"", "", "namespace"},
			&KubeVals{"", "", "sample namespace"},
			&KubeVals{"", "", "sample namespace"},
		},
		{
			"test7",
			&KubeOptions{"kubeconfig", "", ""},
			&KubeVals{"sample kube config", "", ""},
			&KubeVals{"sample kube config", "", defaultIstioNamespace},
		},
		{
			"test8",
			&KubeOptions{"", "context", ""},
			&KubeVals{"", "sample context", ""},
			&KubeVals{"", "sample context", defaultIstioNamespace},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(tt *testing.T) {
			o := RemoteSecretOptions{
				ServiceAccountName: testServiceAccountName,
				AuthType:           RemoteSecretAuthTypeBearerToken,
				KubeOptions: KubeOptions{},
				Type:       "config",
				SecretName: "saSecret",
			}
			flags := pflag.NewFlagSet(tc.name, pflag.ContinueOnError)

			o.addFlags(flags)

			if tc.KubeOpts.Kubeconfig != "" {
				flags.StringVarP(&o.Kubeconfig, tc.KubeOpts.Kubeconfig, "", tc.KubeOptsValue.KubeConfigValue, "")
			}
			if tc.KubeOpts.Context != "" {
				flags.StringVarP(&o.Context, tc.KubeOpts.Context, "", tc.KubeOptsValue.ContextValue, "")
			}
			if tc.KubeOpts.Namespace != "" {
				flags.StringVarP(&o.Namespace, tc.KubeOpts.Namespace, "", tc.KubeOptsValue.NamespaceValue, "")
			}

			o.KubeOptions.prepare(flags)

			if tc.expected.KubeConfigValue != o.Kubeconfig {
				tt.Errorf("test failed for %v", tc.name)
			}
			if tc.expected.ContextValue != o.Context {
				tt.Errorf("test failed for %v", tc.name)
			}
			if tc.expected.NamespaceValue != o.Namespace {
				tt.Errorf("test failed for %v", tc.name)
			}
		})
	}

	f := filenameOption{"test"}
	err := f.prepare()
	if err != nil {
		t.Errorf("Test failed")
	}
}

func TestAddFlags(t *testing.T) {
	o := filenameOption{"test"}
	flags := pflag.NewFlagSet("test", pflag.ContinueOnError)
	o.addFlags(flags)
	expectedFlagUsage := "  -f, --filename string   Filename of the multicluster mesh description\n"
	actualFlagUsage := flags.FlagUsages()
	assert.Equal(t, actualFlagUsage, expectedFlagUsage)
}
