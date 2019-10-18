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
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/security/pkg/k8s/configmap"
)

const (
	fakeRootCA          = "fake root CA"
	testOverrideContext = "test-override-Context"
)

var (
	fakeCAConfigMapGood = &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      configmap.IstioSecurityConfigMapName,
		},
		Data: map[string]string{
			configmap.CATLSRootCertName: fakeRootCA,
		},
	}

	fakeCAConfigMapMissingRoot = &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      configmap.IstioSecurityConfigMapName,
		},
		Data: map[string]string{},
	}
)

func TestCreateTrustAnchorInternal(t *testing.T) {
	cases := []struct {
		name       string
		opts       TrustAnchorOptions
		config     *api.Config
		objs       []runtime.Object
		want       *v1.ConfigMap
		wantErrStr string
	}{
		{
			name: "missing CA configmap",
			opts: TrustAnchorOptions{KubeOptions{Namespace: testNamespace}},
			config: &api.Config{
				CurrentContext: testContext,
				Contexts: map[string]*api.Context{
					testContext: {Cluster: "cluster"},
				},
				Clusters: map[string]*api.Cluster{
					"cluster": {Server: "server"},
				},
			},
			objs: []runtime.Object{
				kubeSystemNamespace,
			},
			wantErrStr: `configmaps "istio-security" not found`,
		},
		{
			name: "missing CA root in configmap",
			opts: TrustAnchorOptions{KubeOptions{Namespace: testNamespace}},
			config: &api.Config{
				CurrentContext: testContext,
				Contexts: map[string]*api.Context{
					testContext: {Cluster: "cluster"},
				},
				Clusters: map[string]*api.Cluster{
					"cluster": {Server: "server"},
				},
			},
			objs: []runtime.Object{
				kubeSystemNamespace,
				fakeCAConfigMapMissingRoot,
			},
			wantErrStr: `"caTLSRootCert" not found in configmap istio-security`,
		},
		{
			name: "success with default Context",
			opts: TrustAnchorOptions{KubeOptions{Namespace: testNamespace}},
			config: &api.Config{
				CurrentContext: testContext,
				Contexts: map[string]*api.Context{
					testContext: {Cluster: "cluster"},
				},
				Clusters: map[string]*api.Cluster{
					"cluster": {Server: "server"},
				},
			},
			objs: []runtime.Object{
				kubeSystemNamespace,
				fakeCAConfigMapGood,
			},
			want: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: trustAnchorNameFromUID(kubeSystemNamespace.UID),
					Annotations: map[string]string{
						"istio.io/clusterContext": testContext,
					},
					Labels: map[string]string{
						"security.istio.io/extra-trust-anchors": "true", // TODO replace with type when trust anchor PR is merged.
					},
				},
				Data: map[string]string{
					string(kubeSystemNamespace.UID): fakeRootCA,
				},
			},
		},
		{
			name: "success with Context override",
			opts: TrustAnchorOptions{
				KubeOptions{
					Context:   testOverrideContext,
					Namespace: testNamespace,
				},
			},
			config: &api.Config{
				CurrentContext: testOverrideContext,
				Contexts: map[string]*api.Context{
					testOverrideContext: {Cluster: "cluster"},
				},
				Clusters: map[string]*api.Cluster{
					"cluster": {Server: "server"},
				},
			},
			objs: []runtime.Object{
				kubeSystemNamespace,
				fakeCAConfigMapGood,
			},
			want: &v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: trustAnchorNameFromUID(kubeSystemNamespace.UID),
					Annotations: map[string]string{
						"istio.io/clusterContext": testOverrideContext,
					},
					Labels: map[string]string{
						"security.istio.io/extra-trust-anchors": "true", // TODO replace with type when trust anchor PR is merged.
					},
				},
				Data: map[string]string{
					string(kubeSystemNamespace.UID): fakeRootCA,
				},
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			client := fake.NewSimpleClientset(c.objs...)
			got, err := createTrustAnchor(client, c.config, c.opts)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but got none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but got %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else if diff := cmp.Diff(got, c.want); diff != "" {
				tt.Errorf("got\n%v\nwant\n%vdiff %v", got, c.want, diff)
			}
		})
	}
}
