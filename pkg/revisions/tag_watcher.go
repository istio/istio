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

const defaultRevisionValidatingWebhookName = "istiod-default-validator"

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

	namespaces    kclient.Client[*corev1.Namespace]
	queue         controllers.Queue
	webhooks      kclient.Client[*admissionregistrationv1.MutatingWebhookConfiguration]
	vWebhooks     kclient.Client[*admissionregistrationv1.ValidatingWebhookConfiguration]
	services      kclient.Client[*corev1.Service]
	webhooksIndex kclient.Index[string, *admissionregistrationv1.MutatingWebhookConfiguration]
	servicesIndex kclient.Index[string, *corev1.Service]
}

func NewTagWatcher(client kube.Client, revision string, systemNamespace string) TagWatcher {
	log.Debugf("Creating tag watcher for revision %s in namespace %s", revision, systemNamespace)
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
	p.webhooksIndex = kclient.CreateStringIndex(p.webhooks, "istioRev",
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

	// Initialize the services client with a namespace filter
	p.services = kclient.NewFiltered[*corev1.Service](client, kubetypes.Filter{
		ObjectFilter: kubetypes.NewStaticObjectFilter(isTagService),
		Namespace:    systemNamespace,
	})
	p.services.AddEventHandler(controllers.ObjectHandler(p.queue.AddObject))

	// Create a service index similar to the webhooks index
	p.servicesIndex = kclient.CreateStringIndex(p.services, "istioRevSvc",
		func(svc *corev1.Service) []string {
			rev := svc.GetLabels()[label.IoIstioRev.Name]
			if rev == "" || !isTagService(svc) {
				return nil
			}
			return []string{rev}
		})

	return p
}

func (p *tagWatcher) Run(stopCh <-chan struct{}) {
	if !kube.WaitForCacheSync("tag watcher", stopCh, p.webhooks.HasSynced, p.services.HasSynced) {
		p.queue.ShutDownEarly()
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
	var weAreDefaultRevision bool
	currentDefaultRevision := p.vWebhooks.Get(defaultRevisionValidatingWebhookName, "")
	if currentDefaultRevision != nil {
		weAreDefaultRevision = currentDefaultRevision.Labels[label.IoIstioRev.Name] == p.revision
	}
	return myTags.Contains(selectedTag) || selectedTag == "" && (myTags.Contains("default") || weAreDefaultRevision)
}

func (p *tagWatcher) GetMyTags() sets.String {
	res := sets.New(p.revision)
	mwhTags := sets.New[string]()
	for _, wh := range p.webhooksIndex.Lookup(p.revision) {
		res.Insert(wh.GetLabels()[label.IoIstioTag.Name])
		mwhTags.Insert(wh.GetLabels()[label.IoIstioTag.Name])
	}
	for _, svc := range p.servicesIndex.Lookup(p.revision) {
		tagName := svc.GetLabels()[label.IoIstioTag.Name]
		res.Insert(tagName)

		if mwhTags.Contains(tagName) {
			log.Debugf("Tag name %s is already defined by a service. Delete the %s-%s MutatingWebhookConfiguration ", tagName, "istio-revision-tag", tagName)
			mwhTags.Delete(tagName)
		}
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

// isTagService filters services based on specific criteria.
func isTagService(uobj any) bool {
	obj := controllers.ExtractObject(uobj)
	if obj == nil {
		return false
	}
	_, ok := obj.GetLabels()[label.IoIstioTag.Name]
	return ok
}
