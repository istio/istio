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
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/api/label"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/kubetypes"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/util/sets"
)

var log = istiolog.RegisterScope("tag-watcher", "Revision tags watcher")

const (
	defaultRevisionValidatingWebhookName = "istiod-default-validator"
	defaultTagMutatingWebhookName        = "istio-revision-tag-default"
)

// TagWatcher keeps track of the current tags and can notify watchers
// when the tags change.
type TagWatcher interface {
	Run(stopCh <-chan struct{})
	HasSynced() bool
	AddHandler(handler TagHandler)
	GetMyTags() sets.String
	IsMine(metav1.ObjectMeta) bool
}

// TagHandler is a callback for when the tags revision change.
type TagHandler func(sets.String)

type tagWatcher struct {
	revision string
	handlers []TagHandler

	namespaces kclient.Client[*corev1.Namespace]
	queue      controllers.Queue
	webhooks   kclient.Client[*admissionregistrationv1.MutatingWebhookConfiguration]
	index      kclient.Index[string, *admissionregistrationv1.MutatingWebhookConfiguration]
	vWebhooks  kclient.Client[*admissionregistrationv1.ValidatingWebhookConfiguration]
}

func NewTagWatcher(client kube.Client, revision string) TagWatcher {
	p := &tagWatcher{
		revision: revision,
	}
	p.queue = controllers.NewQueue("tag", controllers.WithReconciler(func(key types.NamespacedName) error {
		p.notifyHandlers()
		return nil
	}))
	p.webhooks = kclient.NewFiltered[*admissionregistrationv1.MutatingWebhookConfiguration](client, kubetypes.Filter{
		ObjectFilter: kubetypes.NewStaticObjectFilter(isTagWebhook),
	})
	p.webhooks.AddEventHandler(controllers.ObjectHandler(p.queue.AddObject))
	p.index = kclient.CreateStringIndex(p.webhooks,
		func(o *admissionregistrationv1.MutatingWebhookConfiguration) []string {
			rev := o.GetLabels()[label.IoIstioRev.Name]
			if rev == "" || !isTagWebhook(o) {
				return nil
			}
			return []string{rev}
		})

	// Track the default validating webhook to learn what the default revision is
	p.vWebhooks = kclient.NewFiltered[*admissionregistrationv1.ValidatingWebhookConfiguration](client, kubetypes.Filter{
		FieldSelector: "metadata.name=" + defaultRevisionValidatingWebhookName,
	})
	p.vWebhooks.AddEventHandler(controllers.ObjectHandler(p.queue.AddObject))
	p.namespaces = kclient.New[*corev1.Namespace](client)
	return p
}

func (p *tagWatcher) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("tag watcher", stopCh, p.webhooks.HasSynced) {
		return
	}
	// Notify handlers of initial state
	p.notifyHandlers()
	p.queue.Run(stopCh)
}

// AddHandler registers a new handler for updates to tag changes.
func (p *tagWatcher) AddHandler(handler TagHandler) {
	p.handlers = append(p.handlers, handler)
}

func (p *tagWatcher) HasSynced() bool {
	return p.queue.HasSynced()
}

func (p *tagWatcher) IsMine(obj metav1.ObjectMeta) bool {
	selectedTag, ok := obj.Labels[label.IoIstioRev.Name]
	if !ok {
		ns := p.namespaces.Get(obj.Namespace, "")
		if ns == nil {
			return true
		}
		selectedTag = ns.Labels[label.IoIstioRev.Name]
	}
	myTags := p.GetMyTags()
	// Figure out if there is a default tag that doesn't point to us
	var otherDefaultTagExists bool
	if !myTags.Contains("default") {
		defaultTag := p.webhooks.Get(defaultTagMutatingWebhookName, "")
		otherDefaultTagExists = defaultTag != nil
	}
	var weAreDefaultRevision bool
	currentDefaultRevision := p.vWebhooks.Get(defaultRevisionValidatingWebhookName, "")
	if currentDefaultRevision != nil {
		weAreDefaultRevision = currentDefaultRevision.Labels[label.IoIstioRev.Name] == p.revision
	}
	return myTags.Contains(selectedTag) ||
		selectedTag == "" && (myTags.Contains("default") ||
			(weAreDefaultRevision && !otherDefaultTagExists)) // Default tag takes precedence over default revision if they differ
}

func (p *tagWatcher) GetMyTags() sets.String {
	res := sets.New(p.revision)
	for _, wh := range p.index.Lookup(p.revision) {
		res.Insert(wh.GetLabels()[label.IoIstioTag.Name])
	}
	return res
}

// notifyHandlers notifies all registered handlers on tag change.
func (p *tagWatcher) notifyHandlers() {
	myTags := p.GetMyTags()
	log.Debugf("Current tags: %s", myTags)
	currentDefaultRevision := p.vWebhooks.Get(defaultRevisionValidatingWebhookName, "")
	if currentDefaultRevision == nil {
		log.Debugf("No default revision webhook found")
	} else {
		log.Debugf("Default revision is %s", currentDefaultRevision.Labels[label.IoIstioRev.Name])
		if !myTags.Contains("default") && currentDefaultRevision.Labels[label.IoIstioRev.Name] == p.revision {
			// Check and see if there is a tag named "default" that doesn't point to us
			// This isn't valid (except maybe for a short time during a transition), so log a warning
			defaultTag := p.webhooks.Get(defaultTagMutatingWebhookName, "")
			if defaultTag != nil {
				log.Warnf("Default revision is %s, but there is a tag named 'default' pointing to revision %s. " +
					"If using a default tag, do not set the default revision to a different value")
			}
		}
	}

	for _, handler := range p.handlers {
		handler(myTags)
	}
}

func isTagWebhook(uobj any) bool {
	obj := controllers.ExtractObject(uobj)
	if obj == nil {
		return false
	}
	_, ok := obj.GetLabels()[label.IoIstioTag.Name]
	return ok
}
