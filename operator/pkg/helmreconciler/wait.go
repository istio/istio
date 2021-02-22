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
	"strings"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensions "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	kctldeployment "k8s.io/kubectl/pkg/util/deployment"

	"istio.io/istio/operator/pkg/name"
	"istio.io/istio/operator/pkg/object"
	"istio.io/istio/operator/pkg/util/progress"
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
func WaitForResources(objects object.K8sObjects, restConfig *rest.Config, cs kubernetes.Interface,
	waitTimeout time.Duration, dryRun bool, l *progress.ManifestLog) error {
	if dryRun || TestMode {
		return nil
	}

	if err := waitForCRDs(objects, restConfig); err != nil {
		return err
	}

	var notReady []string

	// Check if we are ready immediately, to avoid the 2s delay below when we are already redy
	if ready, _, err := waitForResources(objects, cs, l); err == nil && ready {
		return nil
	}

	errPoll := wait.Poll(2*time.Second, waitTimeout, func() (bool, error) {
		isReady, notReadyObjects, err := waitForResources(objects, cs, l)
		notReady = notReadyObjects
		return isReady, err
	})

	if errPoll != nil {
		msg := fmt.Sprintf("resources not ready after %v: %v\n%s", waitTimeout, errPoll, strings.Join(notReady, "\n"))
		return errors.New(msg)
	}
	return nil
}

func waitForResources(objects object.K8sObjects, cs kubernetes.Interface, l *progress.ManifestLog) (bool, []string, error) {
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
				return false, nil, err
			}
			namespaces = append(namespaces, *namespace)
		case name.PodStr:
			pod, err := cs.CoreV1().Pods(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}
			pods = append(pods, *pod)
		case name.ReplicationControllerStr:
			rc, err := cs.CoreV1().ReplicationControllers(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}
			list, err := getPods(cs, rc.Namespace, rc.Spec.Selector)
			if err != nil {
				return false, nil, err
			}
			pods = append(pods, list...)
		case name.DeploymentStr:
			currentDeployment, err := cs.AppsV1().Deployments(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}
			_, _, newReplicaSet, err := kctldeployment.GetAllReplicaSets(currentDeployment, cs.AppsV1())
			if err != nil || newReplicaSet == nil {
				return false, nil, err
			}
			newDeployment := deployment{
				newReplicaSet,
				currentDeployment,
			}
			deployments = append(deployments, newDeployment)
		case name.DaemonSetStr:
			ds, err := cs.AppsV1().DaemonSets(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}

			daemonsets = append(daemonsets, ds)
		case name.StatefulSetStr:
			sts, err := cs.AppsV1().StatefulSets(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}
			statefulsets = append(statefulsets, sts)
		case name.ReplicaSetStr:
			rs, err := cs.AppsV1().ReplicaSets(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}
			list, err := getPods(cs, rs.Namespace, rs.Spec.Selector.MatchLabels)
			if err != nil {
				return false, nil, err
			}
			pods = append(pods, list...)
		}
	}
	dr, dnr := deploymentsReady(deployments)
	dsr, dsnr := daemonsetsReady(daemonsets)
	stsr, stsnr := statefulsetsReady(statefulsets)
	nsr, nnr := namespacesReady(namespaces)
	pr, pnr := podsReady(pods)
	isReady := dr && nsr && dsr && stsr && pr
	notReady := append(append(append(append(nnr, dnr...), pnr...), dsnr...), stsnr...)
	if !isReady {
		l.ReportWaiting(notReady)
	}
	return isReady, notReady, nil
}

func waitForCRDs(objects object.K8sObjects, restConfig *rest.Config) error {
	var crdNames []string
	for _, o := range object.KindObjects(objects, name.CRDStr) {
		crdNames = append(crdNames, o.Name)
	}
	if len(crdNames) == 0 {
		return nil
	}
	cs, err := apiextensionsclient.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("k8s client error: %s", err)
	}

	errPoll := wait.Poll(cRDPollInterval, cRDPollTimeout, func() (bool, error) {
	descriptor:
		for _, crdName := range crdNames {
			crd, errGet := cs.ApiextensionsV1().CustomResourceDefinitions().Get(context.TODO(), crdName, metav1.GetOptions{})
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

func getPods(client kubernetes.Interface, namespace string, selector map[string]string) ([]corev1.Pod, error) {
	list, err := client.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{
		FieldSelector: fields.Everything().String(),
		LabelSelector: labels.Set(selector).AsSelector().String(),
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

func deploymentsReady(deployments []deployment) (bool, []string) {
	var notReady []string
	for _, v := range deployments {
		if v.replicaSets.Status.ReadyReplicas < *v.deployment.Spec.Replicas {
			notReady = append(notReady, "Deployment/"+v.deployment.Namespace+"/"+v.deployment.Name)
		}
	}
	return len(notReady) == 0, notReady
}

func daemonsetsReady(daemonsets []*appsv1.DaemonSet) (bool, []string) {
	var notReady []string
	for _, ds := range daemonsets {
		// Make sure all the updated pods have been scheduled
		if ds.Spec.UpdateStrategy.Type == appsv1.OnDeleteDaemonSetStrategyType &&
			ds.Status.UpdatedNumberScheduled != ds.Status.DesiredNumberScheduled {
			scope.Infof("DaemonSet is not ready: %s/%s. %d out of %d expected pods have been scheduled",
				ds.Namespace, ds.Name, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled)
			notReady = append(notReady, "DaemonSet/"+ds.Namespace+"/"+ds.Name)
		}
		if ds.Spec.UpdateStrategy.Type == appsv1.RollingUpdateDaemonSetStrategyType {
			maxUnavailable, err := intstr.GetValueFromIntOrPercent(
				ds.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable,
				int(ds.Status.DesiredNumberScheduled),
				true)
			if err != nil {
				// set max unavailable to the number of desired replicas if the value is invalid
				maxUnavailable = int(ds.Status.DesiredNumberScheduled)
			}
			expectedReady := int(ds.Status.DesiredNumberScheduled) - maxUnavailable
			if !(int(ds.Status.NumberReady) >= expectedReady) {
				scope.Infof("DaemonSet is not ready: %s/%s. %d out of %d expected pods are ready",
					ds.Namespace, ds.Name, ds.Status.NumberReady, expectedReady)
				notReady = append(notReady, "DaemonSet/"+ds.Namespace+"/"+ds.Name)
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
