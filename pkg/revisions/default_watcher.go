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

package revisions

import (
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/pkg/log"
)

const (
	defaultTagWebhookName = "istio-revision-tag-default"
)

// DefaultWatcher keeps track of the current default revision and can notify watchers
// when the default revision changes.
type DefaultWatcher interface {
	Run(stopCh <-chan struct{})
	HasSynced() bool
	GetDefault() string
	AddHandler(handler DefaultHandler)
}

// DefaultHandler is a callback for when the default revision changes.
type DefaultHandler func(string)

type defaultWatcher struct {
	revision        string
	defaultRevision string
	handlers        []DefaultHandler

	queue           controllers.Queue
	webhookInformer cache.SharedIndexInformer
	mu              sync.RWMutex
}

func NewDefaultWatcher(client kube.Client, revision string) DefaultWatcher {
	p := &defaultWatcher{
		revision: revision,
		mu:       sync.RWMutex{},
	}
	p.queue = controllers.NewQueue("default revision", controllers.WithReconciler(p.setDefault))
	p.webhookInformer = client.KubeInformer().Admissionregistration().V1().MutatingWebhookConfigurations().Informer()
	p.webhookInformer.AddEventHandler(controllers.FilteredObjectHandler(p.queue.AddObject, isDefaultTagWebhook))

	return p
}

func (p *defaultWatcher) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync(stopCh, p.webhookInformer.HasSynced) {
		log.Errorf("failed to sync default watcher")
		return
	}
	p.queue.Run(stopCh)
}

// GetDefault returns the current default revision.
func (p *defaultWatcher) GetDefault() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.defaultRevision
}

// AddHandler registers a new handler for updates to default revision changes.
func (p *defaultWatcher) AddHandler(handler DefaultHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers = append(p.handlers, handler)
}

func (p *defaultWatcher) HasSynced() bool {
	return p.queue.HasSynced()
}

// notifyHandlers notifies all registered handlers on default revision change.
// assumes externally locked.
func (p *defaultWatcher) notifyHandlers() {
	for _, handler := range p.handlers {
		handler(p.defaultRevision)
	}
}

func (p *defaultWatcher) setDefault(key types.NamespacedName) error {
	revision := ""
	wh, _, _ := p.webhookInformer.GetIndexer().GetByKey(key.Name)
	if wh != nil {
		revision = wh.(metav1.Object).GetLabels()[label.IoIstioRev.Name]
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultRevision = revision
	p.notifyHandlers()
	return nil
}

func isDefaultTagWebhook(obj controllers.Object) bool {
	return obj.GetName() == defaultTagWebhookName
}
