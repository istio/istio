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
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/filesecrets"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/log"
)

type remoteConfig struct {
	Data map[string][]byte
}

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
	root      string
	namespace string

	mu         sync.RWMutex
	collection krt.Collection[filesecrets.KubeconfigEntry]
	started    bool
	pending    []func(types.NamespacedName, controllers.EventType)
}

func newFileConfigSource(root string, namespace string) *fileConfigSource {
	return &fileConfigSource{
		root:      root,
		namespace: namespace,
	}
}

func (f *fileConfigSource) Start(stop <-chan struct{}) {
	f.mu.Lock()
	if f.started {
		f.mu.Unlock()
		return
	}
	f.started = true

	opts := krt.NewOptionsBuilder(stop, "remote-kubeconfig-file", nil)
	collection, err := filesecrets.NewKubeconfigCollection(
		f.root,
		f.namespace,
		opts.WithName("RemoteKubeconfigs")...,
	)
	if err != nil {
		log.Errorf("Failed to initialize file-based remote kubeconfigs from %q: %v", f.root, err)
		collection = krt.NewStaticCollection[filesecrets.KubeconfigEntry](nil, nil, opts.WithName("RemoteKubeconfigs")...)
	}
	f.collection = collection
	pending := f.pending
	f.pending = nil
	f.mu.Unlock()

	for _, handler := range pending {
		f.registerHandler(handler)
	}
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
	lookup := key.Name
	if key.Namespace != "" {
		lookup = key.Namespace + "/" + key.Name
	}
	entry := collection.GetKey(lookup)
	if entry == nil {
		return nil
	}
	return &remoteConfig{Data: map[string][]byte{entry.ClusterID: entry.Kubeconfig}}
}

func (f *fileConfigSource) AddEventHandler(handler func(key types.NamespacedName, event controllers.EventType)) {
	f.mu.Lock()
	if f.collection == nil {
		f.pending = append(f.pending, handler)
		f.mu.Unlock()
		return
	}
	f.mu.Unlock()

	f.registerHandler(handler)
}

func (f *fileConfigSource) registerHandler(handler func(key types.NamespacedName, event controllers.EventType)) {
	if handler == nil {
		return
	}
	f.mu.RLock()
	collection := f.collection
	f.mu.RUnlock()
	if collection == nil {
		return
	}

	collection.Register(func(ev krt.Event[filesecrets.KubeconfigEntry]) {
		item := ev.Latest()
		handler(types.NamespacedName{Name: item.Name, Namespace: item.Namespace}, ev.Event)
	})
}
