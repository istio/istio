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
	corev1 "k8s.io/api/core/v1"
	klabels "k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/cache"

	mesh "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/ambient"
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
	s.disabledSelectors = newAmbientMeshConfig.DisabledSelectors
	s.mu.Unlock()
	s.UpdateConfig()
}

func (s *Server) setupHandlers() {
	s.queue = controllers.NewQueue("ambient",
		controllers.WithReconciler(s.Reconciler),
		controllers.WithMaxAttempts(5),
	)

	ns := s.kubeClient.KubeInformer().Core().V1().Namespaces()
	s.nsLister = ns.Lister()
	ns.Informer().AddEventHandler(controllers.ObjectHandler(s.queue.AddObject))

	s.kubeClient.KubeInformer().Core().V1().Pods().Informer().AddEventHandler(s.newPodInformer())
}

func (s *Server) Run(stop <-chan struct{}) {
	go s.queue.Run(stop)
	<-stop
}

func (s *Server) ReconcileNamespaces() {
	namespaces, err := s.nsLister.List(klabels.Everything())
	if err != nil {
		log.Errorf("Failed to list namespaces: %v", err)
		return
	}
	for _, ns := range namespaces {
		s.queue.AddObject(ns)
	}
}

func (s *Server) Reconciler(name types.NamespacedName) error {
	// If uproxy is not running, we won't requeue the namespace as it will be requeued after uproxy comes online...
	// let's do this to cleanup the logs a bit and drop an info message
	if !s.isUproxyRunning() {
		log.Infof("Cannot reconcile namespace %s as uproxy is not running", name.Name)
		return nil
	}

	log.Infof("Reconciling namespace %s", name.Name)

	ns, err := s.kubeClient.KubeInformer().Core().V1().Namespaces().Lister().Get(name.Name)
	// Ignore not found or deleted namespaces, as the associated pods will be handled by the CNI plugin
	if err != nil || ns == nil {
		if err := controllers.IgnoreNotFound(err); err != nil {
			log.Errorf("Failed to get namespace %s: %v", name.Name, err)
			return err
		}

		return nil
	}

	matchDisabled, err := s.matchesDisabledSelectors(ns.GetLabels())
	if err != nil {
		log.Errorf("Failed to match disabled selectors for namespace %s: %v", name.Name, err)
		return err
	}
	matchAmbient, err := s.matchesAmbientSelectors(ns.GetLabels())
	if err != nil {
		log.Errorf("Failed to match ambient selectors for namespace %s: %v", name.Name, err)
		return err
	}

	pods, err := s.kubeClient.KubeInformer().Core().V1().Pods().Lister().Pods(name.Name).List(klabels.Everything())
	if err != nil {
		log.Errorf("Failed to list pods in namespace %s: %v", name.Name, err)
		return err
	}

	if (s.isAmbientGlobal() || (s.isAmbientNamespaced() && matchAmbient)) && !matchDisabled {
		if ambient.HasLegacyLabel(ns.GetLabels()) {
			log.Errorf(ErrLegacyLabel, name.Name)
			// Don't put the namespace back in queue, if "they" fix the label, it'll be requeued
			return nil
		}
		log.Infof("Namespace %s is enabled in ambient mesh", name.Name)

		for _, pod := range pods {
			if podOnMyNode(pod) && !ambient.PodHasOptOut(pod) {
				log.Debugf("Adding pod to mesh: %s", pod.Name)
				AddPodToMesh(pod, "")
			} else {
				log.Debugf("Pod %s is not on my node, ignoring (on node: %s vs %s)", pod.Name, pod.Spec.NodeName, NodeName)
			}
		}
	} else {
		log.Infof("Namespace %s is disabled from ambient mesh", name.Name)
		for _, pod := range pods {
			if podOnMyNode(pod) {
				log.Debugf("Checking if in ipset and deleting pod: %s", pod.Name)
				DelPodFromMesh(pod)
			} else {
				log.Debugf("Pod %s is not on my node, ignoring (on node: %s vs %s)", pod.Name, pod.Spec.NodeName, NodeName)
			}
		}
	}

	return nil
}

func (s *Server) newPodInformer() *cache.ResourceEventHandlerFuncs {
	return &cache.ResourceEventHandlerFuncs{
		// We only handle existing resources, so if we get an add event,
		// we need to check to see if pod is running, if so, it's safe to
		// assume it's existing and we've restarted.
		//
		// We also watch for uproxy to start, because that means we need to trigger
		// a bunch of iptable and routing changes.
		AddFunc: func(obj interface{}) {
			// @TODO: maybe not using the full pod struct, likely related to
			// https://github.com/solo-io/istio-sidecarless/issues/85
			pod := obj.(*corev1.Pod)

			if pod.GetLabels()["app"] == "uproxy" && podOnMyNode(pod) {
				if pod.Status.Phase != corev1.PodRunning {
					return
				}

				log.WithLabels("type", "add").Infof("uproxy is now running")

				veth, err := getDeviceWithDestinationOf(pod.Status.PodIP)
				if err != nil {
					log.Errorf("Failed to get device for uproxy ip: %v", err)
					return
				}

				captureDNS := getEnvFromPod(pod, "ISTIO_META_DNS_CAPTURE") == "true"
				err = s.CreateRulesOnNode(veth, pod.Status.PodIP, captureDNS)
				if err != nil {
					log.Errorf("Failed to configure node for uproxy: %v", err)
					return
				}

				s.setUproxyRunning(true)
				// Reconile namespaces, as it is possible for the original reconciliation to have failed, and a
				// small pod to have started up before uproxy is running... so we need to go back and make sure we
				// catch the existing pods
				s.ReconcileNamespaces()
			}
		},
		UpdateFunc: func(old, cur interface{}) {
			// @TODO: maybe not using the full pod struct, likely related to
			// https://github.com/solo-io/istio-sidecarless/issues/85
			newPod := cur.(*corev1.Pod)
			oldPod := old.(*corev1.Pod)

			if newPod.GetLabels()["app"] == "uproxy" && podOnMyNode(newPod) {
				// This will catch if uproxy begins running after us... otherwise it gets handled by AddFunc
				if newPod.Status.Phase != corev1.PodRunning || oldPod.Status.Phase == newPod.Status.Phase {
					return
				}

				log.WithLabels("type", "update").Infof("uproxy is now running")

				veth, err := getDeviceWithDestinationOf(newPod.Status.PodIP)
				if err != nil {
					log.Errorf("Failed to get device for uproxy ip: %v", err)
					return
				}

				captureDNS := getEnvFromPod(newPod, "ISTIO_META_DNS_CAPTURE") == "true"
				err = s.CreateRulesOnNode(veth, newPod.Status.PodIP, captureDNS)
				if err != nil {
					log.Errorf("Failed to configure node for uproxy: %v", err)
					return
				}

				s.setUproxyRunning(true)
				// Reconile namespaces, as it is possible for the original reconciliation to have failed, and a
				// small pod to have started up before uproxy is running... so we need to go back and make sure we
				// catch the existing pods
				s.ReconcileNamespaces()
			}

			// Catch pod with opt out applied
			if ambient.PodHasOptOut(newPod) && !ambient.PodHasOptOut(oldPod) && podOnMyNode(newPod) {
				log.Debugf("Pod %s matches opt out, but was not before, removing from mesh", newPod.Name)
				DelPodFromMesh(newPod)
				return
			}
		},
		DeleteFunc: func(obj interface{}) {
			// @TODO: maybe not using the full pod struct, likely related to
			// https://github.com/solo-io/istio-sidecarless/issues/85
			pod := obj.(*corev1.Pod)

			if pod.GetLabels()["app"] == "uproxy" && podOnMyNode(pod) {
				log.WithLabels("type", "delete").Infof("uproxy is now stopped... cleaning up.")
				s.cleanup()
				s.setUproxyRunning(false)
			} else if podOnMyNode(pod) && IsPodInIpset(pod) {
				log.WithLabels("type", "delete").Infof("Pod %s/%s is now stopped... cleaning up.", pod.Namespace, pod.Name)
				DelPodFromMesh(pod)
			}
		},
	}
}
