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

package controllers

import (
	"sync"

	"k8s.io/client-go/tools/cache"
)

// InformerHandler is a simple wrapper around informer event handlers. This allows callers to register
// a variety of event handlers, and then later clean them up without the need to manually track each of them.
type InformerHandler struct {
	mu       sync.Mutex
	cleanups []func()
}

func NewInformerHandler() *InformerHandler {
	return &InformerHandler{}
}

// RegisterEventHandler registers and event handler for the informer. When Cleanup is called, the handler will be removed.
func (i *InformerHandler) RegisterEventHandler(informer cache.SharedInformer, handler cache.ResourceEventHandler) {
	registration, _ := informer.AddEventHandler(handler)
	i.mu.Lock()
	defer i.mu.Unlock()
	i.cleanups = append(i.cleanups, func() {
		_ = informer.RemoveEventHandler(registration)
	})
}

// Cleanup removes all event handlers that have been registered
func (i *InformerHandler) Cleanup() {
	i.mu.Lock()
	defer i.mu.Unlock()
	for _, c := range i.cleanups {
		c()
	}
}

// KeyFunc is the internal API key function that returns "namespace"/"name" or
// "name" if "namespace" is empty
func KeyFunc(name, namespace string) string {
	if len(namespace) == 0 {
		return name
	}
	return namespace + "/" + name
}
