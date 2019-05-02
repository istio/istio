/*
Copyright The Helm Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package kube // import "k8s.io/helm/pkg/kube"

import (
	"time"

	appsv1 "k8s.io/api/apps/v1"
	appsv1beta1 "k8s.io/api/apps/v1beta1"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	"k8s.io/api/core/v1"
	extensions "k8s.io/api/extensions/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	deploymentutil "k8s.io/kubernetes/pkg/controller/deployment/util"
)

// deployment holds associated replicaSets for a deployment
type deployment struct {
	replicaSets *appsv1.ReplicaSet
	deployment  *appsv1.Deployment
}

// waitForResources polls to get the current status of all pods, PVCs, and Services
// until all are ready or a timeout is reached
func (c *Client) waitForResources(timeout time.Duration, created Result) error {
	c.Log("beginning wait for %d resources with timeout of %v", len(created), timeout)

	kcs, err := c.KubernetesClientSet()
	if err != nil {
		return err
	}
	return wait.Poll(2*time.Second, timeout, func() (bool, error) {
		pods := []v1.Pod{}
		services := []v1.Service{}
		pvc := []v1.PersistentVolumeClaim{}
		deployments := []deployment{}
		for _, v := range created {
			switch value := asVersioned(v).(type) {
			case *v1.ReplicationController:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *v1.Pod:
				pod, err := kcs.CoreV1().Pods(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				pods = append(pods, *pod)
			case *appsv1.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *appsv1beta1.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *appsv1beta2.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *extensions.Deployment:
				currentDeployment, err := kcs.AppsV1().Deployments(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				// Find RS associated with deployment
				newReplicaSet, err := deploymentutil.GetNewReplicaSet(currentDeployment, kcs.AppsV1())
				if err != nil || newReplicaSet == nil {
					return false, err
				}
				newDeployment := deployment{
					newReplicaSet,
					currentDeployment,
				}
				deployments = append(deployments, newDeployment)
			case *extensions.DaemonSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1.DaemonSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta2.DaemonSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1.StatefulSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta1.StatefulSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta2.StatefulSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *extensions.ReplicaSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1beta2.ReplicaSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *appsv1.ReplicaSet:
				list, err := getPods(kcs, value.Namespace, value.Spec.Selector.MatchLabels)
				if err != nil {
					return false, err
				}
				pods = append(pods, list...)
			case *v1.PersistentVolumeClaim:
				claim, err := kcs.CoreV1().PersistentVolumeClaims(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				pvc = append(pvc, *claim)
			case *v1.Service:
				svc, err := kcs.CoreV1().Services(value.Namespace).Get(value.Name, metav1.GetOptions{})
				if err != nil {
					return false, err
				}
				services = append(services, *svc)
			}
		}
		isReady := c.podsReady(pods) && c.servicesReady(services) && c.volumesReady(pvc) && c.deploymentsReady(deployments)
		return isReady, nil
	})
}

func (c *Client) podsReady(pods []v1.Pod) bool {
	for _, pod := range pods {
		if !isPodReady(&pod) {
			c.Log("Pod is not ready: %s/%s", pod.GetNamespace(), pod.GetName())
			return false
		}
	}
	return true
}

func (c *Client) servicesReady(svc []v1.Service) bool {
	for _, s := range svc {
		// ExternalName Services are external to cluster so helm shouldn't be checking to see if they're 'ready' (i.e. have an IP Set)
		if s.Spec.Type == v1.ServiceTypeExternalName {
			continue
		}

		// Make sure the service is not explicitly set to "None" before checking the IP
		if s.Spec.ClusterIP != v1.ClusterIPNone && s.Spec.ClusterIP == "" {
			c.Log("Service is not ready: %s/%s", s.GetNamespace(), s.GetName())
			return false
		}
		// This checks if the service has a LoadBalancer and that balancer has an Ingress defined
		if s.Spec.Type == v1.ServiceTypeLoadBalancer && s.Status.LoadBalancer.Ingress == nil {
			c.Log("Service is not ready: %s/%s", s.GetNamespace(), s.GetName())
			return false
		}
	}
	return true
}

func (c *Client) volumesReady(vols []v1.PersistentVolumeClaim) bool {
	for _, v := range vols {
		if v.Status.Phase != v1.ClaimBound {
			c.Log("PersistentVolumeClaim is not ready: %s/%s", v.GetNamespace(), v.GetName())
			return false
		}
	}
	return true
}

func (c *Client) deploymentsReady(deployments []deployment) bool {
	for _, v := range deployments {
		if !(v.replicaSets.Status.ReadyReplicas >= *v.deployment.Spec.Replicas-deploymentutil.MaxUnavailable(*v.deployment)) {
			c.Log("Deployment is not ready: %s/%s", v.deployment.GetNamespace(), v.deployment.GetName())
			return false
		}
	}
	return true
}

func getPods(client kubernetes.Interface, namespace string, selector map[string]string) ([]v1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		LabelSelector: labels.Set(selector).AsSelector().String(),
	})
	return list.Items, err
}

func isPodReady(pod *v1.Pod) bool {
	if &pod.Status != nil && len(pod.Status.Conditions) > 0 {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == v1.PodReady &&
				condition.Status == v1.ConditionTrue {
				return true
			}
		}
	}
	return false
}
