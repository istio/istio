// Copyright Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package ambient

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"

	"istio.io/api/label"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
)

func TestBuildRemoteClustersCollection(t *testing.T) {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "remote-cluster-secret",
			Namespace: "istio-system",
			Labels: map[string]string{
				MultiClusterSecretLabel: "true",
			},
		},
		Data: map[string][]byte{
			"my-cluster": []byte("irrelevant"),
		},
	}
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "istio-system",
			Labels: map[string]string{
				label.TopologyNetwork.Name: "network-1",
			},
		},
	}
	tests := []struct {
		name            string
		options         Options
		filter          kclient.Filter
		configOverrides []func(*rest.Config)
		expectedError   bool
	}{
		{
			name: "successful build",
			options: Options{
				Client:          kube.NewFakeClient(secret),
				ClusterID:       "local-cluster",
				SystemNamespace: "istio-system",
			},
			filter: kclient.Filter{
				Namespace: "istio-system",
			},
			expectedError: false,
		},
		// {
		// 	name: "in-cluster config error",
		// 	options: Options{
		// 		Client:          kube.NewFakeClient(secret),
		// 		ClusterID:       "local-cluster",
		// 		SystemNamespace: "istio-system",
		// 	},
		// 	filter: kclient.Filter{
		// 		Namespace: "istio-system",
		// 	},
		// 	expectedError: true,
		// },
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := krt.NewOptionsBuilder(test.NewStop(t), "test", krt.GlobalDebugHandler)
			builderClient := kube.NewFakeClient(namespace)
			builder := func(kubeConfig []byte, clusterId cluster.ID, configOverrides ...func(*rest.Config)) (kube.Client, error) {
				if tt.expectedError {
					return nil, assert.AnError
				}

				return builderClient, nil
			}
			clusters := buildRemoteClustersCollection(tt.options, opts, builder, tt.filter, tt.configOverrides...)
			tt.options.Client.RunAndWait(opts.Stop()) // Wait for the client in options to be ready
			clusters.WaitUntilSynced(opts.Stop())

			if tt.expectedError {
				assert.Len(t, clusters.List(), 0)
			} else {
				assert.Len(t, clusters.List(), 1)
			}
		})
	}
}
