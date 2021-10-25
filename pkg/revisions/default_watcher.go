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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
)

const (
	defaultTagWebhookName = "istio-revision-tag-default"
)

// DefaultWatcher keeps track of the current default revision and can notify watchers
// when the default revision changes.
type DefaultWatcher interface {
	GetDefault() string
	AddHandler(handler DefaultHandler)
}

// DefaultHandler is a callback for when the default revision changes.
type DefaultHandler func(string)

type defaultWatcher struct {
	revision        string
	defaultRevision string
	handlers        []DefaultHandler
	webhookInformer cache.SharedInformer
	mu              sync.Mutex
}

func NewDefaultWatcher(client kube.Client, revision string) DefaultWatcher {
	p := &defaultWatcher{
		revision: revision,
		mu:       sync.Mutex{},
	}
	p.webhookInformer = client.KubeInformer().Admissionregistration().V1().MutatingWebhookConfigurations().Informer()
	p.webhookInformer.AddEventHandler(p.makeHandler())

	return p
}

func (p *defaultWatcher) makeHandler() *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			meta, _ := meta.Accessor(obj)
			if filterUpdate(meta) {
				return
			}
			p.setDefaultFromLabels(meta.GetLabels())
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			meta, _ := meta.Accessor(newObj)
			if filterUpdate(meta) {
				return
			}
			p.setDefaultFromLabels(meta.GetLabels())
		},
		DeleteFunc: func(obj interface{}) {
			meta, _ := meta.Accessor(obj)
			if filterUpdate(meta) {
				return
			}
			p.mu.Lock()
			p.defaultRevision = ""
			p.notifyHandlers()
			p.mu.Unlock()
		},
	}
}

func filterUpdate(obj metav1.Object) bool {
	return obj.GetName() != defaultTagWebhookName
}

func (p *defaultWatcher) setDefaultFromLabels(labels map[string]string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if revision, ok := labels[label.IoIstioRev.Name]; ok {
		if revision != p.defaultRevision {
			p.defaultRevision = revision
			p.notifyHandlers()
		}
	}
}

// notifyHandlers notifies all registered handlers on default revision change.
// assumes externally locked.
func (p *defaultWatcher) notifyHandlers() {
	for _, handler := range p.handlers {
		handler(p.defaultRevision)
	}
}

// GetDefault returns the current default revision.
func (p *defaultWatcher) GetDefault() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.defaultRevision
}

// AddHandler registers a new handler for updates to default revision changes.
func (p *defaultWatcher) AddHandler(handler DefaultHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers = append(p.handlers, handler)
}
