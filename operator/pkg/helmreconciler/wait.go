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

package helmreconciler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	kctldeployment "k8s.io/kubectl/pkg/util/deployment"

	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/progress"
	"istio.io/istio/pkg/kube"
)

const (
	// defaultWaitResourceTimeout is the maximum wait time for all resources(namespace/deployment/pod) to be created.
	defaultWaitResourceTimeout = 300 * time.Second
	// cRDPollInterval is how often the state of CRDs is polled when waiting for their creation.
	cRDPollInterval = 500 * time.Millisecond
	// cRDPollTimeout is the maximum wait time for all CRDs to be created.
	cRDPollTimeout = 60 * time.Second
)

// deployment holds associated replicaSets for a deployment
type deployment struct {
	replicaSets *appsv1.ReplicaSet
	deployment  *appsv1.Deployment
}

// WaitForResources polls to get the current status of all pods, PVCs, and Services
// until all are ready or a timeout is reached
func WaitForResources(objects object.K8sObjects, client kube.Client,
	waitTimeout time.Duration, dryRun bool, l *progress.ManifestLog) error {
	if dryRun || TestMode {
		return nil
	}

	if err := waitForCRDs(objects, client); err != nil {
		return err
	}

	var notReady []string
	var debugInfo map[string]string

	// Check if we are ready immediately, to avoid the 2s delay below when we are already redy
	if ready, _, _, err := waitForResources(objects, client.Kube(), l); err == nil && ready {
		return nil
	}

	errPoll := wait.Poll(2*time.Second, waitTimeout, func() (bool, error) {
		isReady, notReadyObjects, debugInfoObjects, err := waitForResources(objects, client.Kube(), l)
		notReady = notReadyObjects
		debugInfo = debugInfoObjects
		return isReady, err
	})

	messages := []string{}
	for _, id := range notReady {
		debug, f := debugInfo[id]
		if f {
			messages = append(messages, fmt.Sprintf("  %s (%s)", id, debug))
		} else {
			messages = append(messages, fmt.Sprintf("  %s", debug))
		}
	}
	if errPoll != nil {
		msg := fmt.Sprintf("resources not ready after %v: %v\n%s", waitTimeout, errPoll, strings.Join(messages, "\n"))
		return fmt.Errorf(msg)
	}
	return nil
}

func waitForResources(objects object.K8sObjects, cs kubernetes.Interface, l *progress.ManifestLog) (bool, []string, map[string]string, error) {
	pods := []corev1.Pod{}
	deployments := []deployment{}
	daemonsets := []*appsv1.DaemonSet{}
	statefulsets := []*appsv1.StatefulSet{}
	namespaces := []corev1.Namespace{}

	for _, o := range objects {
		kind := o.GroupVersionKind().Kind
		switch kind {
		case name.NamespaceStr:
			namespace, err := cs.CoreV1().Namespaces().Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, nil, err
			}
			namespaces = append(namespaces, *namespace)
		case name.DeploymentStr:
			currentDeployment, err := cs.AppsV1().Deployments(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, nil, err
			}
			_, _, newReplicaSet, err := kctldeployment.GetAllReplicaSets(currentDeployment, cs.AppsV1())
			if err != nil || newReplicaSet == nil {
				return false, nil, nil, err
			}
			newDeployment := deployment{
				newReplicaSet,
				currentDeployment,
			}
			deployments = append(deployments, newDeployment)
		case name.DaemonSetStr:
			ds, err := cs.AppsV1().DaemonSets(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, nil, err
			}
			daemonsets = append(daemonsets, ds)
		case name.StatefulSetStr:
			sts, err := cs.AppsV1().StatefulSets(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, nil, err
			}
			statefulsets = append(statefulsets, sts)
		}
	}

	resourceDebugInfo := map[string]string{}
	dr, dnr := deploymentsReady(cs, deployments, resourceDebugInfo)
	dsr, dsnr := daemonsetsReady(daemonsets)
	stsr, stsnr := statefulsetsReady(statefulsets)
	nsr, nnr := namespacesReady(namespaces)
	pr, pnr := podsReady(pods)
	isReady := dr && nsr && dsr && stsr && pr
	notReady := append(append(append(append(nnr, dnr...), pnr...), dsnr...), stsnr...)
	if !isReady {
		l.ReportWaiting(notReady)
	}
	return isReady, notReady, resourceDebugInfo, nil
}

func waitForCRDs(objects object.K8sObjects, client kube.Client) error {
	var crdNames []string
	for _, o := range object.KindObjects(objects, name.CRDStr) {
		crdNames = append(crdNames, o.Name)
	}
	if len(crdNames) == 0 {
		return nil
	}

	errPoll := wait.Poll(cRDPollInterval, cRDPollTimeout, func() (bool, error) {
	descriptor:
		for _, crdName := range crdNames {
			crd, errGet := client.Ext().ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crdName, metav1.GetOptions{})
			if errGet != nil {
				return false, errGet
			}
			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case apiextensions.Established:
					if cond.Status == apiextensions.ConditionTrue {
						scope.Infof("established CRD %s", crdName)
						continue descriptor
					}
				case apiextensions.NamesAccepted:
					if cond.Status == apiextensions.ConditionFalse {
						scope.Warnf("name conflict for %v: %v", crdName, cond.Reason)
					}
				}
			}
			scope.Infof("missing status condition for %q", crdName)
			return false, nil
		}
		return true, nil
	})

	if errPoll != nil {
		scope.Errorf("failed to verify CRD creation; %s", errPoll)
		return fmt.Errorf("failed to verify CRD creation: %s", errPoll)
	}

	scope.Info("Finished applying CRDs.")
	return nil
}

func getPods(client kubernetes.Interface, namespace string, selector labels.Selector) ([]corev1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		LabelSelector: selector.String(),
	})
	return list.Items, err
}

func namespacesReady(namespaces []corev1.Namespace) (bool, []string) {
	var notReady []string
	for _, namespace := range namespaces {
		if namespace.Status.Phase != corev1.NamespaceActive {
			notReady = append(notReady, "Namespace/"+namespace.Name)
		}
	}
	return len(notReady) == 0, notReady
}

func podsReady(pods []corev1.Pod) (bool, []string) {
	var notReady []string
	for _, pod := range pods {
		if !isPodReady(&pod) {
			notReady = append(notReady, "Pod/"+pod.Namespace+"/"+pod.Name)
		}
	}
	return len(notReady) == 0, notReady
}

func isPodReady(pod *corev1.Pod) bool {
	if len(pod.Status.Conditions) > 0 {
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady &&
				condition.Status == corev1.ConditionTrue {
				return true
			}
		}
	}
	return false
}

func deploymentsReady(cs kubernetes.Interface, deployments []deployment, info map[string]string) (bool, []string) {
	var notReady []string
	for _, v := range deployments {
		if v.replicaSets.Status.ReadyReplicas >= *v.deployment.Spec.Replicas {
			// Ready
			continue
		}
		id := "Deployment/" + v.deployment.Namespace + "/" + v.deployment.Name
		notReady = append(notReady, id)
		failure := extractPodFailureReason(cs, v.deployment.Namespace, v.deployment.Spec.Selector)
		if failure != "" {
			info[id] = failure
		}
	}
	return len(notReady) == 0, notReady
}

func extractPodFailureReason(client kubernetes.Interface, namespace string, selector *metav1.LabelSelector) string {
	sel, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return fmt.Sprintf("failed to get label selector: %v", err)
	}
	pods, err := getPods(client, namespace, sel)
	if err != nil {
		return fmt.Sprintf("failed to fetch pods: %v", err)
	}
	sort.Slice(pods, func(i, j int) bool {
		return pods[i].CreationTimestamp.After(pods[j].CreationTimestamp.Time)
	})
	for _, pod := range pods {
		for _, cs := range pod.Status.ContainerStatuses {
			if cs.State.Waiting != nil {
				return fmt.Sprintf("container failed to start: %v: %v", cs.State.Waiting.Reason, cs.State.Waiting.Message)
			}
		}
		if c := getCondition(pod.Status.Conditions, corev1.PodReady); c != nil && c.Status == corev1.ConditionFalse {
			return fmt.Sprintf(c.Message)
		}
	}
	return ""
}

func getCondition(conditions []corev1.PodCondition, condition corev1.PodConditionType) *corev1.PodCondition {
	for _, cond := range conditions {
		if cond.Type == condition {
			return &cond
		}
	}
	return nil
}

func daemonsetsReady(daemonsets []*appsv1.DaemonSet) (bool, []string) {
	var notReady []string
	for _, ds := range daemonsets {
		// Check if the wanting generation is same as the observed generation
		// Only when the observed generation is the same as the generation,
		// other checks will make sense. If not the same, daemon set is not
		// ready
		if ds.Status.ObservedGeneration != ds.Generation {
			scope.Infof("DaemonSet is not ready: %s/%s. Observed generation: %d expected generation: %d",
				ds.Namespace, ds.Name, ds.Status.ObservedGeneration, ds.Generation)
			notReady = append(notReady, "DaemonSet/"+ds.Namespace+"/"+ds.Name)
		} else {
			// Make sure all the updated pods have been scheduled
			if ds.Spec.UpdateStrategy.Type == appsv1.OnDeleteDaemonSetStrategyType &&
				ds.Status.UpdatedNumberScheduled != ds.Status.DesiredNumberScheduled {
				scope.Infof("DaemonSet is not ready: %s/%s. %d out of %d expected pods have been scheduled",
					ds.Namespace, ds.Name, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled)
				notReady = append(notReady, "DaemonSet/"+ds.Namespace+"/"+ds.Name)
			}
			if ds.Spec.UpdateStrategy.Type == appsv1.RollingUpdateDaemonSetStrategyType {
				if ds.Status.DesiredNumberScheduled <= 0 {
					// If DesiredNumberScheduled less then or equal 0, there some cases:
					// 1) daemenset is just created
					// 2) daemonset desired no pod
					// 3) somebody changed it manually
					// All the case is not a ready signal
					scope.Infof("DaemonSet is not ready: %s/%s. Initializing, no pods is running",
						ds.Namespace, ds.Name)
					notReady = append(notReady, "DaemonSet/"+ds.Namespace+"/"+ds.Name)
				} else if ds.Status.NumberReady < ds.Status.DesiredNumberScheduled {
					// Make sure every node has a ready pod
					scope.Infof("DaemonSet is not ready: %s/%s. %d out of %d expected pods are ready",
						ds.Namespace, ds.Name, ds.Status.NumberReady, ds.Status.UpdatedNumberScheduled)
					notReady = append(notReady, "DaemonSet/"+ds.Namespace+"/"+ds.Name)
				}
			}
		}
	}
	return len(notReady) == 0, notReady
}

func statefulsetsReady(statefulsets []*appsv1.StatefulSet) (bool, []string) {
	var notReady []string
	for _, sts := range statefulsets {
		// Make sure all the updated pods have been scheduled
		if sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType &&
			sts.Status.UpdatedReplicas != sts.Status.Replicas {
			scope.Infof("StatefulSet is not ready: %s/%s. %d out of %d expected pods have been scheduled",
				sts.Namespace, sts.Name, sts.Status.UpdatedReplicas, sts.Status.Replicas)
			notReady = append(notReady, "StatefulSet/"+sts.Namespace+"/"+sts.Name)
		}
		if sts.Spec.UpdateStrategy.Type == appsv1.RollingUpdateStatefulSetStrategyType {
			// Dereference all the pointers because StatefulSets like them
			var partition int
			// default replicasfor sts is 1
			replicas := 1
			// the rollingUpdate field can be nil even if the update strategy is a rolling update.
			if sts.Spec.UpdateStrategy.RollingUpdate != nil &&
				sts.Spec.UpdateStrategy.RollingUpdate.Partition != nil {
				partition = int(*sts.Spec.UpdateStrategy.RollingUpdate.Partition)
			}
			if sts.Spec.Replicas != nil {
				replicas = int(*sts.Spec.Replicas)
			}
			expectedReplicas := replicas - partition
			// Make sure all the updated pods have been scheduled
			if int(sts.Status.UpdatedReplicas) != expectedReplicas {
				scope.Infof("StatefulSet is not ready: %s/%s. %d out of %d expected pods have been scheduled",
					sts.Namespace, sts.Name, sts.Status.UpdatedReplicas, expectedReplicas)
				notReady = append(notReady, "StatefulSet/"+sts.Namespace+"/"+sts.Name)
				continue
			}
		}
	}
	return len(notReady) == 0, notReady
}
