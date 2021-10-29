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
	"time"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/util/workqueue"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/pkg/log"
)

const (
	defaultTagWebhookName = "istio-revision-tag-default"
	initSignal            = "INIT"
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

	queue           workqueue.RateLimitingInterface
	webhookInformer cache.SharedInformer
	initialSync     *atomic.Bool
	mu              sync.RWMutex
}

func NewDefaultWatcher(client kube.Client, revision string) DefaultWatcher {
	p := &defaultWatcher{
		revision:    revision,
		queue:       workqueue.NewRateLimitingQueue(workqueue.DefaultControllerRateLimiter()),
		initialSync: atomic.NewBool(false),
		mu:          sync.RWMutex{},
	}
	p.webhookInformer = client.KubeInformer().Admissionregistration().V1().MutatingWebhookConfigurations().Informer()
	p.webhookInformer.AddEventHandler(p.makeHandler())

	return p
}

func (p *defaultWatcher) Run(stopCh <-chan struct{}) {
	defer utilruntime.HandleCrash()
	defer p.queue.ShutDown()
	if !kube.WaitForCacheSyncInterval(stopCh, time.Second, p.webhookInformer.HasSynced) {
		log.Errorf("failed to sync default watcher")
		return
	}
	p.queue.Add(initSignal)
	go wait.Until(p.runQueue, time.Second, stopCh)
	<-stopCh
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
	return p.initialSync.Load()
}

func (p *defaultWatcher) runQueue() {
	for p.processNextItem() {
	}
}

func (p *defaultWatcher) processNextItem() bool {
	item, quit := p.queue.Get()
	if quit {
		log.Debug("default watcher shutting down, returning")
		return false
	}
	defer p.queue.Done(item)

	if item.(string) == initSignal {
		p.initialSync.Store(true)
	} else {
		p.setDefault(item.(string))
	}

	return true
}

func (p *defaultWatcher) makeHandler() *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			meta, _ := meta.Accessor(obj)
			if filterUpdate(meta) {
				return
			}
			p.queue.Add(getDefault(meta))
		},
		UpdateFunc: func(oldObj interface{}, newObj interface{}) {
			meta, _ := meta.Accessor(newObj)
			if filterUpdate(meta) {
				return
			}
			p.queue.Add(getDefault(meta))
		},
		DeleteFunc: func(obj interface{}) {
			meta, _ := meta.Accessor(obj)
			if filterUpdate(meta) {
				return
			}
			// treat "" to mean no default revision is set
			p.queue.Add("")
		},
	}
}

// notifyHandlers notifies all registered handlers on default revision change.
// assumes externally locked.
func (p *defaultWatcher) notifyHandlers() {
	for _, handler := range p.handlers {
		handler(p.defaultRevision)
	}
}

func getDefault(meta metav1.Object) string {
	if revision, ok := meta.GetLabels()[label.IoIstioRev.Name]; ok {
		return revision
	}
	return ""
}

func (p *defaultWatcher) setDefault(revision string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.defaultRevision = revision
	p.notifyHandlers()
}

func filterUpdate(obj metav1.Object) bool {
	return obj.GetName() != defaultTagWebhookName
}
