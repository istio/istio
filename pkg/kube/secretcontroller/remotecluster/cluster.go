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

package remotecluster

import (
	"crypto/sha256"
	"errors"
	"fmt"

	"go.uber.org/atomic"
	"k8s.io/client-go/tools/clientcmd"

	"istio.io/istio/pkg/kube"
)

// Cluster encapsulates a "remote" cluster accessed by the control plane with it's kubeclient and status
// of syncing infomers registered to that kube client.
type Cluster struct {
	ClusterID     string
	KubeConfigSha [sha256.Size]byte

	// Client for accessing the cluster.
	Client kube.Client
	// Stop channel which is closed when the cluster is removed or the secretcontroller that created the client is stopped.
	// Client.RunAndWait is called using this channel.
	Stop chan struct{}
	// initialSync is marked when RunAndWait completes
	initialSync *atomic.Bool
	// SyncTimeout is marked after features.RemoteClusterTimeout
	SyncTimeout *atomic.Bool
}

// New initializes a Cluster and its kube client
func New(kubeConfig []byte, clusterID string, syncTimeoutMarker *atomic.Bool) (*Cluster, error) {
	clients, err := BuildClientsFromConfig(kubeConfig)
	if err != nil {
		return nil, err
	}
	return &Cluster{
		ClusterID: clusterID,
		Client:    clients,
		// access outside this package should only be reading
		Stop: make(chan struct{}),
		// for use inside the package, to close on cleanup
		initialSync:   atomic.NewBool(false),
		SyncTimeout:   syncTimeoutMarker,
		KubeConfigSha: sha256.Sum256(kubeConfig),
	}, nil
}

// Run starts the cluster's informers and waits for caches to sync. Once caches are synced, we mark the cluster synced.
// This should be called after each of the handlers have registered informers, and should be run in a goroutine.
func (r *Cluster) Run() {
	r.Client.RunAndWait(r.Stop)
	r.initialSync.Store(true)
}

func (r *Cluster) HasSynced() bool {
	return r.initialSync.Load() || r.SyncTimeout.Load()
}

func (r *Cluster) SyncDidTimeout() bool {
	return r.SyncTimeout.Load() && !r.HasSynced()
}

// BuildClientsFromConfig creates kube.Clients from the provided kubeconfig. This is overiden for testing only
var BuildClientsFromConfig = func(kubeConfig []byte) (kube.Client, error) {
	if len(kubeConfig) == 0 {
		return nil, errors.New("kubeconfig is empty")
	}

	rawConfig, err := clientcmd.Load(kubeConfig)
	if err != nil {
		return nil, fmt.Errorf("kubeconfig cannot be loaded: %v", err)
	}

	if err := clientcmd.Validate(*rawConfig); err != nil {
		return nil, fmt.Errorf("kubeconfig is not valid: %v", err)
	}

	clientConfig := clientcmd.NewDefaultClientConfig(*rawConfig, &clientcmd.ConfigOverrides{})

	clients, err := kube.NewClient(clientConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create kube clients: %v", err)
	}
	return clients, nil
}
