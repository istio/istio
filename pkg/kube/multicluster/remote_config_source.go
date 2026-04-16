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

package multicluster

import (
	"fmt"
	"sort"
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
)

type remoteConfig struct {
	Data map[string][]byte
	Err  error
}

// remoteConfigSource abstracts how remote cluster kubeconfigs are discovered
// (Kubernetes Secrets or local filesystem) behind a uniform event/get interface.
type remoteConfigSource interface {
	AddEventHandler(handler func(key types.NamespacedName, event controllers.EventType))
	HasSynced() bool
	Start(stop <-chan struct{})
	Get(key types.NamespacedName) *remoteConfig
}

type secretConfigSource struct {
	client kclient.Client[*corev1.Secret]
}

func newSecretConfigSource(client kclient.Client[*corev1.Secret]) *secretConfigSource {
	return &secretConfigSource{client: client}
}

func (s *secretConfigSource) AddEventHandler(handler func(key types.NamespacedName, event controllers.EventType)) {
	s.client.AddEventHandler(controllers.EventHandler[*corev1.Secret]{
		AddFunc: func(obj *corev1.Secret) {
			handler(types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, controllers.EventAdd)
		},
		UpdateFunc: func(_, obj *corev1.Secret) {
			handler(types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, controllers.EventUpdate)
		},
		DeleteFunc: func(obj *corev1.Secret) {
			handler(types.NamespacedName{Name: obj.Name, Namespace: obj.Namespace}, controllers.EventDelete)
		},
	})
}

func (s *secretConfigSource) HasSynced() bool {
	return s.client.HasSynced()
}

func (s *secretConfigSource) Start(stop <-chan struct{}) {
	s.client.Start(stop)
}

func (s *secretConfigSource) Get(key types.NamespacedName) *remoteConfig {
	secret := s.client.Get(key.Name, key.Namespace)
	if secret == nil {
		return nil
	}
	return &remoteConfig{Data: secret.Data}
}

type fileConfigSource struct {
	root string

	mu         sync.RWMutex
	collection krt.Collection[KubeconfigFile]
	startOnce  sync.Once
	// deferredHandlers stores AddEventHandler registrations made before Start() initializes the collection.
	// They are registered against the collection once Start() finishes creating it.
	deferredHandlers []func(types.NamespacedName, controllers.EventType)
}

func newFileConfigSource(root string) *fileConfigSource {
	return &fileConfigSource{
		root: root,
	}
}

func (f *fileConfigSource) Start(stop <-chan struct{}) {
	f.startOnce.Do(func() {
		opts := krt.NewOptionsBuilder(stop, "remote-kubeconfig-file", nil)
		collection, err := NewKubeconfigCollection(
			f.root,
			opts.WithName("RemoteKubeconfigs")...,
		)
		if err != nil {
			log.Errorf(
				"Failed to initialize file-backed remote kubeconfigs from %q: %v; "+
					"using an empty static collection instead. "+
					"File-backed remote cluster discovery will not recover until istiod restarts.",
				f.root,
				err,
			)
			// Fall back to a synced dummy collection so the secret controller can
			// continue starting even if the initial file-backed source setup fails.
			collection = krt.NewStaticCollection[KubeconfigFile](nil, nil, opts.WithName("RemoteKubeconfigs")...)
		}

		f.mu.Lock()
		f.collection = collection
		deferredHandlers := f.deferredHandlers
		f.deferredHandlers = nil
		f.mu.Unlock()

		// Register deferred handlers that arrived before the collection was initialized.
		for _, handler := range deferredHandlers {
			registerHandler(collection, handler)
		}
	})
}

func (f *fileConfigSource) HasSynced() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if f.collection == nil {
		return false
	}
	return f.collection.HasSynced()
}

func (f *fileConfigSource) Get(key types.NamespacedName) *remoteConfig {
	f.mu.RLock()
	collection := f.collection
	f.mu.RUnlock()
	if collection == nil {
		return nil
	}

	entry, err := getClusterEntry(collection, key.Name)
	if err != nil {
		return &remoteConfig{Err: err}
	}
	if entry == nil {
		return nil
	}
	return &remoteConfig{Data: map[string][]byte{entry.ClusterID: entry.Kubeconfig}}
}

func (f *fileConfigSource) AddEventHandler(handler func(key types.NamespacedName, event controllers.EventType)) {
	if handler == nil {
		return
	}

	f.mu.Lock()
	collection := f.collection
	if collection == nil {
		f.deferredHandlers = append(f.deferredHandlers, handler)
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	registerHandler(collection, handler)
}

// registerHandler adapts KRT file collection events into controller queue keys.
// KRT keys kubeconfigs by hash, but the controller reconciles by semantic cluster ID,
// so updates enqueue old and new cluster IDs when they differ. This ensures a kubeconfig
// file changing from cluster A to cluster B reconciles both keys so the old
// cluster can be deleted and the new cluster can be added.
func registerHandler(collection krt.Collection[KubeconfigFile], handler func(key types.NamespacedName, event controllers.EventType)) {
	if handler == nil {
		return
	}
	if collection == nil {
		return
	}

	collection.Register(func(ev krt.Event[KubeconfigFile]) {
		oldClusterID := ""
		if ev.Old != nil {
			oldClusterID = ev.Old.ClusterID
		}
		newClusterID := ""
		if ev.New != nil {
			newClusterID = ev.New.ClusterID
		}

		switch {
		case oldClusterID != "" && newClusterID != "" && oldClusterID != newClusterID:
			handler(types.NamespacedName{Name: oldClusterID, Namespace: ""}, ev.Event)
			handler(types.NamespacedName{Name: newClusterID, Namespace: ""}, ev.Event)
		case newClusterID != "":
			handler(types.NamespacedName{Name: newClusterID, Namespace: ""}, ev.Event)
		case oldClusterID != "":
			handler(types.NamespacedName{Name: oldClusterID, Namespace: ""}, ev.Event)
		}
	})
}

// getClusterEntry resolves a file-backed kubeconfig by semantic cluster ID.
// The underlying KRT collection is keyed by kubeconfig hash,
// so multiple files may refer to the same cluster. In that case we report a
// conflict so the controller logs it and keeps the existing cluster rather than
// deleting it or switching to a conflicting config.
func getClusterEntry(collection krt.Collection[KubeconfigFile], clusterID string) (*KubeconfigFile, error) {
	var selected *KubeconfigFile
	hashes := make([]string, 0, 2)
	for _, item := range collection.List() {
		if item.ClusterID != clusterID {
			continue
		}
		if selected == nil {
			selected = &item
		}
		hashes = append(hashes, item.KubeconfigHash)
	}
	if len(hashes) > 1 {
		sort.Strings(hashes)
		return nil, fmt.Errorf("multiple file-backed kubeconfigs found for cluster %q with hashes %v", clusterID, hashes)
	}
	return selected, nil
}
