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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	informersv1 "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	listerv1 "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/ambient/ambientpod"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
)

var ErrLegacyLabel = "Namespace %s has sidecar label istio-injection or istio.io/rev " +
	"enabled while also setting ambient mode. This is not supported and the namespace will " +
	"be ignored from the ambient mesh."

func (s *Server) newConfigMapWatcher() {
	var newAmbientMeshConfig *mesh.MeshConfig_AmbientMeshConfig

	if s.environment.Mesh().AmbientMesh == nil {
		newAmbientMeshConfig = &mesh.MeshConfig_AmbientMeshConfig{
			Mode: mesh.MeshConfig_AmbientMeshConfig_DEFAULT,
		}
	} else {
		newAmbientMeshConfig = s.environment.Mesh().AmbientMesh
	}

	if s.meshMode != newAmbientMeshConfig.Mode {
		log.Infof("Ambient mesh mode changed from %s to %s",
			s.meshMode, newAmbientMeshConfig.Mode)
		s.ReconcileNamespaces()
	}
	s.mu.Lock()
	s.meshMode = newAmbientMeshConfig.Mode
	s.disabledSelectors = ambientpod.ConvertDisabledSelectors(newAmbientMeshConfig.DisabledSelectors)
	s.marshalableDisabledSelectors = newAmbientMeshConfig.DisabledSelectors
	s.mu.Unlock()
	s.UpdateConfig()
}

func (s *Server) setupHandlers() {
	s.queue = controllers.NewQueue("ambient",
		controllers.WithGenericReconciler(s.Reconcile),
		controllers.WithMaxAttempts(5),
	)

	// We only need to handle pods on our node
	podInformer := s.kubeClient.KubeInformer().InformerFor(&corev1.Pod{}, func(k kubernetes.Interface, resync time.Duration) cache.SharedIndexInformer {
		return informersv1.NewFilteredPodInformer(
			k, metav1.NamespaceAll, resync, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
			func(options *metav1.ListOptions) {
				options.FieldSelector = "spec.nodeName=" + NodeName
			},
		)
	})
	_ = podInformer.SetTransform(kube.StripUnusedFields)
	_, _ = podInformer.AddEventHandler(controllers.FromEventHandler(func(o controllers.Event) {
		s.queue.Add(o)
	}))
	s.podLister = listerv1.NewPodLister(podInformer.GetIndexer())

	// Namespaces could be anything though, so we watch all of those
	ns := s.kubeClient.KubeInformer().Core().V1().Namespaces()
	s.nsLister = ns.Lister()
	_, _ = ns.Informer().AddEventHandler(controllers.ObjectHandler(s.EnqueueNamespace))
}

func (s *Server) Run(stop <-chan struct{}) {
	go s.queue.Run(stop)
	<-stop
}

func (s *Server) ReconcileNamespaces() {
	namespaces, _ := s.nsLister.List(klabels.Everything())
	for _, ns := range namespaces {
		s.EnqueueNamespace(ns)
	}
}

// EnqueueNamespace takes a Namespace and enqueues all Pod objects that make need an update
func (s *Server) EnqueueNamespace(o controllers.Object) {
	nsLabels := o.GetLabels()
	namespace := o.GetName()
	matchDisabled := s.matchesDisabledSelectors(nsLabels)
	matchAmbient := s.matchesAmbientSelectors(nsLabels)
	pods, _ := s.podLister.Pods(namespace).List(klabels.Everything())
	if (s.isAmbientGlobal() || (s.isAmbientNamespaced() && matchAmbient)) && !matchDisabled {
		if ambientpod.HasLegacyLabel(nsLabels) {
			log.Errorf(ErrLegacyLabel, namespace)
			return
		}
		log.Infof("Namespace %s is enabled in ambient mesh", namespace)
		for _, pod := range pods {
			s.queue.Add(controllers.Event{
				New:   pod,
				Old:   pod,
				Event: controllers.EventUpdate,
			})
		}
	} else {
		log.Infof("Namespace %s is disabled from ambient mesh", namespace)
		for _, pod := range pods {
			s.queue.Add(controllers.Event{
				New:   pod,
				Event: controllers.EventDelete,
			})
		}
	}
}

func (s *Server) Reconcile(input any) error {
	event := input.(controllers.Event)
	log := log.WithLabels("type", event.Event)
	pod := event.Latest().(*corev1.Pod)
	if ztunnelPod(pod) {
		return s.ReconcileZtunnel()
	}
	switch event.Event {
	case controllers.EventAdd:
	case controllers.EventUpdate:
		// For update, we just need to handle opt outs
		newPod := event.New.(*corev1.Pod)
		oldPod := event.Old.(*corev1.Pod)
		if ambientpod.PodHasOptOut(newPod) && !ambientpod.PodHasOptOut(oldPod) {
			log.Debugf("Pod %s matches opt out, but was not before, removing from mesh", newPod.Name)
			DelPodFromMesh(s.kubeClient.Kube(), newPod)
			return nil
		}

		if event.New == event.Old {
			// This is a bit of a hack, but in this case we are queued from namespace handler
			if ambientpod.PodHasOptOut(pod) {
				log.Debugf("Pod %s matches opt out, skipping", pod.Name)
				return nil
			}
			AddPodToMesh(s.kubeClient.Kube(), pod, "")
		}
	case controllers.EventDelete:
		if IsPodInIpset(pod) {
			log.Infof("Pod %s/%s is now stopped... cleaning up.", pod.Namespace, pod.Name)
			DelPodFromMesh(s.kubeClient.Kube(), pod)
		}
		return nil
	}
	if ambientpod.PodHasOptOut(pod) {
		log.Debugf("Pod %s matches opt out, skipping", pod.Name)
		return nil
	}
	return nil
}

func ztunnelPod(pod *corev1.Pod) bool {
	return pod.GetLabels()["app"] == "ztunnel"
}
