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

package filesecrets

import (
	"fmt"
	"path/filepath"
	"testing"

	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/file"
)

func TestClusterIDFromKubeconfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *clientcmdapi.Config
		want    string
		wantErr bool
	}{
		{
			name: "current context",
			cfg: &clientcmdapi.Config{
				CurrentContext: "ctx",
				Contexts: map[string]*clientcmdapi.Context{
					"ctx": {Cluster: "cluster-a"},
				},
			},
			want: "cluster-a",
		},
		{
			name: "single context",
			cfg: &clientcmdapi.Config{
				Contexts: map[string]*clientcmdapi.Context{
					"ctx": {Cluster: "cluster-b"},
				},
			},
			want: "cluster-b",
		},
		{
			name: "single cluster",
			cfg: &clientcmdapi.Config{
				Clusters: map[string]*clientcmdapi.Cluster{
					"cluster-c": {Server: "https://cluster-c.example.com"},
				},
			},
			want: "cluster-c",
		},
		{
			name:    "no clusters",
			cfg:     &clientcmdapi.Config{},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clusterIDFromKubeconfig(tc.cfg)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, got, tc.want)
		})
	}
}

func TestParseKubeconfig(t *testing.T) {
	data := kubeconfigYAML("cluster-1")
	files, err := parseKubeconfig(data)
	assert.NoError(t, err)
	assert.Equal(t, len(files), 1)
	assert.Equal(t, files[0].ClusterID, "cluster-1")
	assert.Equal(t, files[0].Kubeconfig, data)
}

func TestNewKubeconfigCollection(t *testing.T) {
	stop := test.NewStop(t)
	root := t.TempDir()

	collection, err := NewKubeconfigCollection(root, krt.WithStop(stop))
	assert.NoError(t, err)
	assert.Equal(t, collection.WaitUntilSynced(stop), true)
	assert.Equal(t, collection.HasSynced(), true)

	tracker := assert.NewTracker[string](t)
	collection.Register(func(ev krt.Event[KubeconfigFile]) {
		tracker.Record(fmt.Sprintf("%s/%s", ev.Event.String(), ev.Latest().ResourceName()))
	})

	file.WriteOrFail(t, filepath.Join(root, "remote.yaml"), kubeconfigYAML("cluster-1"))
	tracker.WaitOrdered("add/cluster-1")
}

func kubeconfigYAML(clusterID string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: %s
  cluster:
    server: https://%s.example.com
contexts:
- name: %s-context
  context:
    cluster: %s
    user: %s-user
current-context: %s-context
users:
- name: %s-user
  user:
    token: token
`, clusterID, clusterID, clusterID, clusterID, clusterID, clusterID, clusterID))
}
