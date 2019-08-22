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

package multicluster

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"
	"text/template"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd/api"
)

var secretTemplate = `# Remote pilot credentials for cluster context "{{ .Name }}"
apiVersion: v1
kind: Secret
metadata:
  creationTimestamp: null
  labels:
    istio.io/remote-multi-cluster: "true"
    istio/multiCluster: "true"
  name: istio-pilot-remote-secret-{{ .Name }}
stringData:
  {{ .Name }}: |
    apiVersion: v1
    clusters:
    - cluster:
        certificate-authority-data: {{ .CADataBase64 }}
        server: {{ .Server }}
      name: {{ .Name }}
    contexts:
    - context:
        cluster: {{ .Name }}
        user: {{ .Name }}
      name: {{ .Name }}
    current-context: {{ .Name }}
    kind: Config
    preferences: {}
    users:
    - name: {{ .Name }}
      user:
        token: {{ .Token }}
---
`

type testClusterData struct {
	// Pilot SA data
	Name         string
	CAData       string
	CADataBase64 string
	Token        string

	// Secret with Pilot SA encoded in kubeconfig
	kubeconfigSecretYaml string

	Server string
}

func makeTestClusterData(name string) *testClusterData {
	caData := fmt.Sprintf("caData-%v", name)
	token := fmt.Sprintf("token-%v", name)
	d := &testClusterData{
		Name:         name,
		CAData:       caData,
		CADataBase64: base64.StdEncoding.EncodeToString([]byte(caData)),
		Token:        token,
		Server:       fmt.Sprintf("server-%v", name),
	}

	var out bytes.Buffer
	tmpl, err := template.New("secretTemplate").Parse(secretTemplate)
	if err != nil {
		panic(err)
	}
	if err := tmpl.Execute(&out, d); err != nil {
		panic(err)
	}

	d.kubeconfigSecretYaml = out.String()

	return d
}

func makeServiceAccount(name string, secrets ...string) *v1.ServiceAccount {
	sa := &v1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
	}

	for _, secret := range secrets {
		sa.Secrets = append(sa.Secrets, v1.ObjectReference{
			Name:      secret,
			Namespace: testNamespace,
		})
	}

	return sa
}

func makeSecret(name, caData, token string) *v1.Secret {
	out := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: testNamespace,
		},
		Data: map[string][]byte{},
	}
	if len(caData) > 0 {
		out.Data[caDataSecretKey] = []byte(caData)
	}
	if len(token) > 0 {
		out.Data[tokenSecretKey] = []byte(token)
	}
	return out
}

var (
	testNamespace = "istio-system-test"

	c0 = makeTestClusterData("c0")
	c1 = makeTestClusterData("c1")
	c2 = makeTestClusterData("c2")
)

var testAPIConfig = &api.Config{
	Clusters: map[string]*api.Cluster{
		"c0": {Server: c0.Server},
		"c1": {Server: c1.Server},
		"c2": {Server: c2.Server},
	},
	AuthInfos: map[string]*api.AuthInfo{},
	Contexts: map[string]*api.Context{
		"c0": {Cluster: "c0"},
		"c1": {Cluster: "c1"},
		"c2": {Cluster: "c2"},
	},
}

func TestCreatePilotRemoteSecrets(t *testing.T) {
	prevStartingConfig := newStartingConfig
	defer func() { newStartingConfig = prevStartingConfig }()

	prevKubernetesInteface := newKubernetesInterface
	defer func() { newKubernetesInterface = prevKubernetesInteface }()

	cases := []struct {
		name string

		// test input
		contexts []string
		config   *api.Config
		objs     map[string][]runtime.Object

		badStartingConfig bool

		want       string
		wantErrStr string
	}{
		{
			name:       "no clusters",
			contexts:   []string{},
			wantErrStr: "no remote cluster contexts specified",
		},
		{
			name:       "invalid cluster",
			contexts:   []string{"bad"},
			config:     testAPIConfig,
			wantErrStr: "does not exist in config",
		},
		{
			name:     "single valid cluster",
			contexts: []string{"c0"},
			config:   testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount(defaultServiceAccountName, "secret0"),
					makeSecret("secret0", c0.CAData, c0.Token),
				},
			},
			want: outputHeader + c0.kubeconfigSecretYaml,
		},
		{
			name:     "three valid cluster",
			contexts: []string{"c0", "c1", "c2"},
			config:   testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount(defaultServiceAccountName, "secret0"),
					makeSecret("secret0", c0.CAData, c0.Token),
				},
				"c1": {
					makeServiceAccount(defaultServiceAccountName, "secret1"),
					makeSecret("secret1", c1.CAData, c1.Token),
				},
				"c2": {
					makeServiceAccount(defaultServiceAccountName, "secret2"),
					makeSecret("secret2", c2.CAData, c2.Token),
				},
			},
			want: outputHeader + c0.kubeconfigSecretYaml + c1.kubeconfigSecretYaml + c2.kubeconfigSecretYaml,
		},
		{
			name:     "missing service account",
			contexts: []string{"c0"},
			config:   testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount("missing-service-acount", "secret0"),
					makeSecret("secret1", c0.CAData, c0.Token),
				},
			},
			wantErrStr: "failed to get serviceaccount",
		},
		{
			name:     "wrong secret reference",
			contexts: []string{"c0"},
			config:   testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount(defaultServiceAccountName, "secret0"),
					makeSecret("missing-secret1", c0.CAData, c0.Token),
				},
			},
			wantErrStr: "failed to get secret",
		},
		{
			name:     "zero secret references",
			contexts: []string{"c0"},
			config:   testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount(defaultServiceAccountName),
					makeSecret("secret0", c0.CAData, c0.Token),
				},
			},
			wantErrStr: "wrong number of secrets",
		},
		{
			name:     "more than one secret references",
			contexts: []string{"c0"},
			config:   testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount(defaultServiceAccountName, "secret0", "secret0"),
					makeSecret("secret1", c0.CAData, c0.Token),
				},
			},
			wantErrStr: "wrong number of secrets",
		},
		{
			name:              "error on bad starting config",
			contexts:          []string{"c0", "c1", "c2S"},
			badStartingConfig: true,
			config:            testAPIConfig,
			objs: map[string][]runtime.Object{
				"c0": {
					makeServiceAccount(defaultServiceAccountName, "secret0"),
					makeSecret("secret0", c0.CAData, c0.Token),
				},
				"c1": {
					makeServiceAccount(defaultServiceAccountName, "bad-secret1"),
					makeSecret("secret1", c1.CAData, c1.Token),
				},
			},
			wantErrStr: "bad starting config",
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			newStartingConfig = func(_ string) (*api.Config, error) {
				if c.badStartingConfig {
					return nil, errors.New("bad starting config")
				}
				return c.config, nil
			}

			newKubernetesInterface = func(kubeconfig, context string) (kubernetes.Interface, error) {
				objs, ok := c.objs[context]
				if !ok {
					return nil, fmt.Errorf("context %v does not exist in config", context)
				}
				return fake.NewSimpleClientset(objs...), nil
			}

			got, err := createPilotRemoteSecrets(options{
				secretPrefix:       defaultSecretPrefix,
				serviceAccountName: defaultServiceAccountName,
				secretLabels:       defaultSecretlabels,
				namespace:          testNamespace,
				args:               c.contexts,
			})
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			}

			if diff := cmp.Diff(got, c.want); diff != "" {
				tt.Errorf("got\n%v\nwant\n%vdiff %v", got, c.want, diff)
			}
		})
	}
}

func TestCreateRemotePilotKubeconfig(t *testing.T) {
	cases := []struct {
		name       string
		in         *v1.Secret
		config     *api.Config
		want       *api.Config
		context    string
		wantErrStr string
	}{
		{
			name: "missing context",
			in:   makeSecret("s0", c0.CAData, c0.Token),
			config: &api.Config{
				Contexts: map[string]*api.Context{},
			},
			context:    "c0",
			wantErrStr: "context not found in kubeconfig",
		},
		{
			name: "missing cluster",
			in:   makeSecret("s0", c0.CAData, c0.Token),
			config: &api.Config{
				Contexts: map[string]*api.Context{"c0": {Cluster: "c0"}},
				Clusters: map[string]*api.Cluster{},
			},
			context:    "c0",
			wantErrStr: "cluster not found in kubeconfig",
		},
		{
			name: "missing caData",
			in:   makeSecret("s0", "", c0.Token),
			config: &api.Config{
				Contexts: map[string]*api.Context{"c0": {Cluster: "c0"}},
				Clusters: map[string]*api.Cluster{"c0": {Server: "c0"}},
			},
			context:    "c0",
			wantErrStr: fmt.Sprintf("no %q data found in secret", caDataSecretKey),
		},
		{
			name: "missing token",
			in:   makeSecret("s0", c0.CADataBase64, ""),
			config: &api.Config{
				Contexts: map[string]*api.Context{"c0": {Cluster: "c0"}},
				Clusters: map[string]*api.Cluster{"c0": {Server: "c0"}},
			},
			context:    "c0",
			wantErrStr: fmt.Sprintf("no %q data found in secret", tokenSecretKey),
		},
		{
			name: "missing success",
			in:   makeSecret("s0", c0.CADataBase64, c0.Token),
			config: &api.Config{
				Contexts: map[string]*api.Context{"c0": {Cluster: "c0"}},
				Clusters: map[string]*api.Cluster{"c0": {Server: "c0"}},
			},
			context: "c0",
			want: &api.Config{
				Contexts: map[string]*api.Context{
					"c0": {
						Cluster:  "c0",
						AuthInfo: "c0",
					},
				},
				Clusters: map[string]*api.Cluster{
					"c0": {
						Server:                   "c0",
						CertificateAuthorityData: []byte(c0.CADataBase64),
					},
				},
				AuthInfos: map[string]*api.AuthInfo{
					"c0": {
						Token: c0.Token,
					},
				},
				CurrentContext: "c0",
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			got, err := createRemotePilotKubeconfig(c.in, c.config, c.context)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("got success but expected error to contain %v", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("got error %q but expected it to contain %v", err, c.wantErrStr)
				}
			} else if err != nil {
				tt.Fatalf("got error %v but wanted success", err)
			} else if diff := cmp.Diff(got, c.want); diff != "" {
				tt.Fatalf(" got %v\nwant %v\ndiff %v", got, c.want, diff)
			}
		})
	}
}
