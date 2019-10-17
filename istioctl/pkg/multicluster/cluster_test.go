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
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube/secretcontroller"
)

const (
	testNetwork = "test-network"
)

var (
	kubeSystemNamespaceUID = types.UID("54643f96-eca0-11e9-bb97-42010a80000a")
	kubeSystemNamespace    = &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kube-system",
			UID:  kubeSystemNamespaceUID,
		},
	}

	pilotDeployment = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-pilot",
			Namespace: testNamespace,
		},
	}

	pilotDeploymentDefaultIstioSystem = &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "istio-pilot",
			Namespace: defaultIstioNamespace,
		},
	}
)

func TestClusterUID(t *testing.T) {
	client := fake.NewSimpleClientset()
	if _, err := clusterUID(client); err == nil {
		t.Errorf("clusterUID should fail when kube-system namespace is missing")
	}

	want := kubeSystemNamespaceUID
	client = fake.NewSimpleClientset(kubeSystemNamespace)
	got, err := clusterUID(client)
	if err != nil {
		t.Fatalf("clusterUID failed: %v", err)
	}
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

var (
	goodClusterDesc = ClusterDesc{
		Network:              testNetwork,
		Namespace:            testNamespace,
		ServiceAccountReader: testServiceAccountName,
	}

	clusterDescFieldsNotSet = ClusterDesc{
		Network: testNetwork,
	}
	clusterDescWithDefaults = ClusterDesc{
		Network:              testNetwork,
		ServiceAccountReader: defaultServiceAccountReader,
		Namespace:            defaultIstioNamespace,
	}
)

func TestNewCluster(t *testing.T) {
	cases := []struct {
		name                    string
		objs                    []runtime.Object
		context                 string
		desc                    ClusterDesc
		injectClientCreateError error
		want                    *Cluster
		wantErrStr              string
	}{
		{
			name: "missing defaults",
			objs: []runtime.Object{
				kubeSystemNamespace,
				pilotDeploymentDefaultIstioSystem,
			},
			context: testContext,
			desc:    clusterDescFieldsNotSet,
			want: &Cluster{
				ClusterDesc: clusterDescWithDefaults,
				context:     testContext,
				uid:         kubeSystemNamespaceUID,
				installed:   true,
			},
		},
		{
			name: "create client failure",
			objs: []runtime.Object{
				kubeSystemNamespace,
				pilotDeployment,
			},
			injectClientCreateError: errors.New("failed to create client"),
			context:                 testContext,
			desc:                    goodClusterDesc,
			wantErrStr:              "failed to create client",
		},
		{
			name: "uid lookup failed",
			objs: []runtime.Object{
				pilotDeployment,
			},
			context:    testContext,
			desc:       goodClusterDesc,
			wantErrStr: `namespaces "kube-system" not found`,
		},
		{
			name: "istio not installed",
			objs: []runtime.Object{
				kubeSystemNamespace,
			},
			context: testContext,
			desc:    goodClusterDesc,
			want: &Cluster{
				ClusterDesc: goodClusterDesc,
				context:     testContext,
				uid:         kubeSystemNamespaceUID,
				installed:   false,
			},
		},
		{
			name: "success",
			objs: []runtime.Object{
				kubeSystemNamespace,
				pilotDeployment,
			},
			context: testContext,
			desc:    goodClusterDesc,
			want: &Cluster{
				ClusterDesc: goodClusterDesc,
				context:     testContext,
				uid:         kubeSystemNamespaceUID,
				installed:   true,
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			env := newFakeEnvironmentOrDie(t, api.NewConfig(), c.objs...)
			env.injectClientCreateError = c.injectClientCreateError

			got, err := NewCluster(c.context, c.desc, env)
			if c.wantErrStr != "" {
				if err == nil {
					tt.Fatalf("wanted error including %q but got none", c.wantErrStr)
				} else if !strings.Contains(err.Error(), c.wantErrStr) {
					tt.Fatalf("wanted error including %q but got %v", c.wantErrStr, err)
				}
			} else if c.wantErrStr == "" && err != nil {
				tt.Fatalf("wanted non-error but got %q", err)
			} else {
				c.want.client = env.client
				if !reflect.DeepEqual(got, c.want) {
					tt.Fatalf("\n got %#v\nwant %#v", *got, *c.want)
				}
			}
		})
	}
}

func createTestClusterAndEnvOrDie(t *testing.T,
	context string,
	config *api.Config,
	desc ClusterDesc,
	objs ...runtime.Object,
) (*fakeEnvironment, *Cluster) {
	t.Helper()

	env := newFakeEnvironmentOrDie(t, config, objs...)
	c, err := NewCluster(context, desc, env)
	if err != nil {
		t.Fatalf("could not create test cluster: %v", err)
	}
	return env, c
}

var (
	testSecretLabeled0 = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSecretLabeled0",
			Namespace: testNamespace,
			Labels:    map[string]string{secretcontroller.MultiClusterSecretLabel: "true"},
		},
	}
	testSecretLabeled1 = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSecretLabeled1",
			Namespace: testNamespace,
			Labels:    map[string]string{secretcontroller.MultiClusterSecretLabel: "true"},
		},
	}
	testSecretNotLabeled0 = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "testSecretNotLabeld0",
			Namespace: testNamespace,
		},
	}
)

func TestReadRemoteSecrets(t *testing.T) {
	cases := []struct {
		name              string
		objs              []runtime.Object
		context           string
		desc              ClusterDesc
		injectListFailure error
		want              remoteSecrets
	}{
		{
			name: "list failed",
			objs: []runtime.Object{
				kubeSystemNamespace,
				pilotDeployment,
				testSecretLabeled0,
				testSecretLabeled1,
				testSecretNotLabeled0,
			},
			injectListFailure: errors.New("list failed"),
			context:           testContext,
			desc:              goodClusterDesc,
			want:              remoteSecrets{},
		},
		{
			name: "no labeled secrets",
			objs: []runtime.Object{
				kubeSystemNamespace,
				pilotDeployment,
				testSecretNotLabeled0,
			},
			context: testContext,
			desc:    goodClusterDesc,
			want:    remoteSecrets{},
		},
		{
			name: "success",
			objs: []runtime.Object{
				kubeSystemNamespace,
				pilotDeployment,
				testSecretLabeled0,
				testSecretLabeled1,
				testSecretNotLabeled0,
			},
			context: testContext,
			desc:    goodClusterDesc,
			want: remoteSecrets{
				types.UID(testSecretLabeled0.Name): testSecretLabeled0,
				types.UID(testSecretLabeled1.Name): testSecretLabeled1,
			},
		},
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			env, cluster := createTestClusterAndEnvOrDie(t, c.context, nil, c.desc, c.objs...)
			if c.injectListFailure != nil {
				reaction := func(action ktesting.Action) (handled bool, ret runtime.Object, err error) {
					return true, nil, c.injectListFailure
				}
				env.client.PrependReactor("list", "secrets", reaction)
			}
			got := cluster.readRemoteSecrets(env)
			if !reflect.DeepEqual(got, c.want) {
				tt.Fatalf("\n got %#v\nwant %#v", got, c.want)
			}
		})
	}
}

func TestReadCACerts(t *testing.T) {

}

func TestReadIngressGatewayAddresses(t *testing.T) {

}
