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
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/clientcmd/api"
)

func makeTestOptions(args []string) options {
	return options{
		secretPrefix:       defaultSecretPrefix,
		serviceAccountName: defaultServiceAccountName,
		secretLabels:       defaultSecretlabels,
		args:               args,
	}
}

func TestCreatePilotRemoteSecrets(t *testing.T) {
	prevStartingConfig := newStartingConfig
	defer func() { newStartingConfig = prevStartingConfig }()

	prevKubernetesInteface := newKubernetesInterface
	defer func() { newKubernetesInterface = prevKubernetesInteface }()

	cases := []struct {
		name string

		// test input
		o      options
		config *api.Config
		obj    []runtime.Object

		want    string
		wantErr bool
	}{
		{
			name:    "no clusters",
			o:       makeTestOptions(nil),
			wantErr: true, // no remote cluster contexts specified
		},
		// TODO add more tests
	}

	for i := range cases {
		c := &cases[i]
		t.Run(fmt.Sprintf("[%v] %v", i, c.name), func(tt *testing.T) {
			newStartingConfig = func(_ string) (*api.Config, error) {
				return c.config, nil
			}

			newKubernetesInterface = func(kubeconfig, context string) (kubernetes.Interface, error) {
				return fake.NewSimpleClientset(c.obj...), nil
			}

			got, err := createPilotRemoteSecrets(c.o)
			if c.wantErr && err == nil {
				t.Fatal("wanted error but got none")
			} else if !c.wantErr && err != nil {
				t.Fatalf("wanted non-error but got %q", err)
			}

			if got != c.want {
				t.Errorf("got %v \nwant %v", got, c.want)
			}
		})
	}
}
