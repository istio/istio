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

package ambient

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"

	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/util/sets"
)

func (s *Server) setupHandlers() {
	s.queue = controllers.NewQueue("ambient",
		controllers.WithGenericReconciler(s.Reconcile),
		controllers.WithMaxAttempts(5),
	)

	// We only need to handle pods on our node
	s.pods = kclient.NewFiltered[*corev1.Pod](s.kubeClient, kclient.Filter{FieldSelector: "spec.nodeName=" + NodeName})
	s.pods.AddEventHandler(controllers.FromEventHandler(func(o controllers.Event) {
		s.queue.Add(o)
	}))

	// Namespaces could be anything though, so we watch all of those
	s.namespaces = kclient.New[*corev1.Namespace](s.kubeClient)
	s.namespaces.AddEventHandler(controllers.EventHandler[*corev1.Namespace]{
		AddFunc: func(ns *corev1.Namespace) {
			s.EnqueueNamespace(ns)
		},
		UpdateFunc: func(oldNs, newNs *corev1.Namespace) {
			if oldNs.Labels[constants.DataplaneMode] != newNs.Labels[constants.DataplaneMode] {
				s.EnqueueNamespace(newNs)
			}
		},
	})
}

func (s *Server) Run(stop <-chan struct{}) {
	go s.queue.Run(stop)
	<-stop
}

func (s *Server) ReconcileNamespaces() sets.Set[string] {
	processed := sets.New[string]()
	for _, ns := range s.namespaces.List(metav1.NamespaceAll, klabels.Everything()) {
		processed.Merge(s.enqueueNamespace(ns))
	}
	return processed
}

// EnqueueNamespace takes a Namespace and enqueues all Pod objects that make need an update
// TODO it is sort of pointless/confusing/implicit to populate Old and New with the same reference here
func (s *Server) EnqueueNamespace(o controllers.Object) {
	s.enqueueNamespace(o)
}

func (s *Server) enqueueNamespace(o controllers.Object) sets.Set[string] {
	namespace := o.GetName()
	matchAmbient := o.GetLabels()[constants.DataplaneMode] == constants.DataplaneModeAmbient
	processed := sets.New[string]()
	if matchAmbient {
		log.Infof("Namespace %s is enabled in ambient mesh", namespace)
	} else {
		log.Infof("Namespace %s is disabled from ambient mesh", namespace)
	}
	for _, pod := range s.pods.List(namespace, klabels.Everything()) {
		// ztunnel pods are never "added to/removed from the mesh", so do not fire
		// spurious events for them to avoid triggering extra
		// ztunnel node reconciliation checks.
		if !ztunnelPod(pod) {
			s.queue.Add(controllers.Event{
				New:   pod,
				Old:   pod,
				Event: controllers.EventUpdate,
			})
			processed.Insert(pod.Status.PodIP)
		}
	}
	return processed
}

func (s *Server) Reconcile(input any) error {
	event := input.(controllers.Event)
	log := log.WithLabels("type", event.Event)
	pod := event.Latest().(*corev1.Pod)
	if ztunnelPod(pod) {
		return s.UpdateActiveNodeProxy()
	}
	switch event.Event {
	case controllers.EventAdd:
	case controllers.EventUpdate:
		// For update, we just need to handle opt outs
		newPod := event.New.(*corev1.Pod)
		oldPod := event.Old.(*corev1.Pod)
		ns := s.namespaces.Get(newPod.Namespace, "")
		if ns == nil {
			return fmt.Errorf("failed to find namespace %v", ns)
		}
		wasEnabled := oldPod.Annotations[constants.AmbientRedirection] == constants.AmbientRedirectionEnabled
		nowEnabled := PodRedirectionEnabled(ns, newPod)
		if wasEnabled && !nowEnabled {
			log.Debugf("Pod %s no longer matches, removing from mesh", newPod.Name)
			s.DelPodFromMesh(newPod, event)
		}

		if !wasEnabled && nowEnabled {
			log.Debugf("Pod %s now matches, adding to mesh", newPod.Name)
			s.AddPodToMesh(pod)
		}
	case controllers.EventDelete:
		s.DelPodFromMesh(pod, event)
	}
	return nil
}
