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

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/istioctl/pkg/tag"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	"istio.io/pkg/log"
)

// TagWatcher keeps track of the current tags and can notify watchers
// when the tags change.
type TagWatcher interface {
	Run(stopCh <-chan struct{})
	HasSynced() bool
	AddHandler(handler TagHandler)
}

// TagHandler is a callback for when the tags revision change.
type TagHandler func([]string)

type tagWatcher struct {
	revision string
	handlers []TagHandler

	queue           controllers.Queue
	webhookInformer kclient.Client[*admissionregistrationv1.MutatingWebhookConfiguration]
	mu              sync.RWMutex
	tagsToRevisions map[string]string
	revisionsToTags map[string][]string
}

func NewTagWatcher(client kube.Client, revision string) TagWatcher {
	p := &tagWatcher{
		revision:        revision,
		mu:              sync.RWMutex{},
		tagsToRevisions: map[string]string{},
		revisionsToTags: map[string][]string{},
	}
	p.queue = controllers.NewQueue("tag", controllers.WithReconciler(p.updateTags))
	p.webhookInformer = kclient.NewFiltered[*admissionregistrationv1.MutatingWebhookConfiguration](client, kubetypes.Filter{
		ObjectFilter:    isTagWebhook,
		ObjectTransform: kube.StripUnusedFields,
	})
	p.webhookInformer.AddEventHandler(controllers.ObjectHandler(p.queue.AddObject))

	return p
}

func (p *tagWatcher) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync(stopCh, p.webhookInformer.HasSynced) {
		log.Errorf("failed to sync tag watcher")
		return
	}

	p.queue.Run(stopCh)
}

// AddHandler registers a new handler for updates to tag changes.
func (p *tagWatcher) AddHandler(handler TagHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers = append(p.handlers, handler)
}

func (p *tagWatcher) HasSynced() bool {
	return p.queue.HasSynced()
}

// notifyHandlers notifies all registered handlers on tag change.
// assumes externally locked.
func (p *tagWatcher) notifyHandlers() {
	for _, handler := range p.handlers {
		handler(append(p.revisionsToTags[p.revision], p.revision))
	}
}

func (p *tagWatcher) updateTags(key types.NamespacedName) error {
	var revision, tagName string
	wh := p.webhookInformer.Get(key.Name, "")
	p.webhookInformer.Get(key.Name, "")
	if wh != nil {
		revision = wh.GetLabels()[label.IoIstioRev.Name]
		tagName = wh.GetLabels()[tag.IstioTagLabel]
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tagsToRevisions[tagName] = revision
	reverseMap := map[string][]string{}
	for key, val := range p.tagsToRevisions {
		reverseMap[val] = append(reverseMap[val], key)
	}
	p.revisionsToTags = reverseMap
	p.notifyHandlers()
	return nil
}

func isTagWebhook(uobj any) bool {
	obj, ok := uobj.(controllers.Object)
	if !ok {
		return false
	}
	_, ok = obj.GetLabels()[tag.IstioTagLabel]
	return ok
}
