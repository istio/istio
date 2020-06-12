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

/*
Copyright 2016 The Kubernetes Authors.

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

package helmreconciler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	appsclient "k8s.io/client-go/kubernetes/typed/apps/v1"
	"k8s.io/client-go/rest"

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
	if dryRun {
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
	pods := make([]corev1.Pod, 0)
	deployments := make([]deployment, 0)
	namespaces := make([]corev1.Namespace, 0)

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
			_, _, newReplicaSet, err := getAllReplicaSets(currentDeployment, cs.AppsV1())
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
			list, err := getPods(cs, ds.Namespace, ds.Spec.Selector.MatchLabels)
			if err != nil {
				return false, nil, err
			}
			pods = append(pods, list...)
		case name.StatefulSetStr:
			sts, err := cs.AppsV1().StatefulSets(o.Namespace).Get(context.TODO(), o.Name, metav1.GetOptions{})
			if err != nil {
				return false, nil, err
			}
			list, err := getPods(cs, sts.Namespace, sts.Spec.Selector.MatchLabels)
			if err != nil {
				return false, nil, err
			}
			pods = append(pods, list...)
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
	nsr, nnr := namespacesReady(namespaces)
	pr, pnr := podsReady(pods)
	isReady := dr && nsr && pr
	notReady := append(append(nnr, dnr...), pnr...)
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
			crd, errGet := cs.ApiextensionsV1beta1().CustomResourceDefinitions().Get(context.TODO(), crdName, metav1.GetOptions{})
			if errGet != nil {
				return false, errGet
			}
			for _, cond := range crd.Status.Conditions {
				switch cond.Type {
				case v1beta1.Established:
					if cond.Status == v1beta1.ConditionTrue {
						scope.Infof("established CRD %s", crdName)
						continue descriptor
					}
				case v1beta1.NamesAccepted:
					if cond.Status == v1beta1.ConditionFalse {
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
	if list != nil {
		return list.Items, err
	}
	return nil, err
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

// getAllReplicaSets returns the old and new replica sets targeted by the given Deployment. It gets PodList and
// ReplicaSetList from client interface. Note that the first set of old replica sets doesn't include the ones
// with no pods, and the second set of old replica sets include all old replica sets. The third returned value
// is the new replica set, and it may be nil if it doesn't exist yet.
func getAllReplicaSets(deployment *appsv1.Deployment,
	c appsclient.ReplicaSetsGetter) ([]*appsv1.ReplicaSet, []*appsv1.ReplicaSet, *appsv1.ReplicaSet, error) {
	rsList, err := listReplicaSets(deployment, rsListFromClient(c))
	if err != nil {
		return nil, nil, nil, err
	}
	newRS := findNewReplicaSet(deployment, rsList)
	oldRSes, allOldRSes := findOldReplicaSets(rsList, newRS)
	return oldRSes, allOldRSes, newRS, nil
}

// RsListFromClient returns an rsListFunc that wraps the given client.
func rsListFromClient(c appsclient.ReplicaSetsGetter) rsListFunc {
	return func(namespace string, options metav1.ListOptions) ([]*appsv1.ReplicaSet, error) {
		rsList, err := c.ReplicaSets(namespace).List(context.TODO(), options)
		if err != nil {
			return nil, err
		}
		var ret []*appsv1.ReplicaSet
		for i := range rsList.Items {
			ret = append(ret, &rsList.Items[i])
		}
		return ret, err
	}
}

// TODO: switch this to full namespacers
type rsListFunc func(string, metav1.ListOptions) ([]*appsv1.ReplicaSet, error)

// listReplicaSets returns a slice of RSes the given deployment targets.
// Note that this does NOT attempt to reconcile ControllerRef (adopt/orphan),
// because only the controller itself should do that.
// However, it does filter out anything whose ControllerRef doesn't match.
func listReplicaSets(deployment *appsv1.Deployment, getRSList rsListFunc) ([]*appsv1.ReplicaSet, error) {
	// TODO: Right now we list replica sets by their labels. We should list them by selector, i.e. the replica set's selector
	//       should be a superset of the deployment's selector, see https://github.com/kubernetes/kubernetes/issues/19830.
	namespace := deployment.Namespace
	selector, err := metav1.LabelSelectorAsSelector(deployment.Spec.Selector)
	if err != nil {
		return nil, err
	}
	options := metav1.ListOptions{LabelSelector: selector.String()}
	all, err := getRSList(namespace, options)
	if err != nil {
		return nil, err
	}
	// Only include those whose ControllerRef matches the Deployment.
	owned := make([]*appsv1.ReplicaSet, 0, len(all))
	for _, rs := range all {
		if metav1.IsControlledBy(rs, deployment) {
			owned = append(owned, rs)
		}
	}
	return owned, nil
}

// EqualIgnoreHash returns true if two given podTemplateSpec are equal, ignoring the diff in value of Labels[pod-template-hash]
// We ignore pod-template-hash because:
// 1. The hash result would be different upon podTemplateSpec API changes
//    (e.g. the addition of a new field will cause the hash code to change)
// 2. The deployment template won't have hash labels
func equalIgnoreHash(template1, template2 *corev1.PodTemplateSpec) bool {
	t1Copy := template1.DeepCopy()
	t2Copy := template2.DeepCopy()
	// Remove hash labels from template.Labels before comparing
	delete(t1Copy.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	delete(t2Copy.Labels, appsv1.DefaultDeploymentUniqueLabelKey)
	return apiequality.Semantic.DeepEqual(t1Copy, t2Copy)
}

// FindNewReplicaSet returns the new RS this given deployment targets (the one with the same pod template).
func findNewReplicaSet(deployment *appsv1.Deployment, rsList []*appsv1.ReplicaSet) *appsv1.ReplicaSet {
	sort.Sort(replicaSetsByCreationTimestamp(rsList))
	for i := range rsList {
		if equalIgnoreHash(&rsList[i].Spec.Template, &deployment.Spec.Template) {
			// In rare cases, such as after cluster upgrades, Deployment may end up with
			// having more than one new ReplicaSets that have the same template as its template,
			// see https://github.com/kubernetes/kubernetes/issues/40415
			// We deterministically choose the oldest new ReplicaSet.
			return rsList[i]
		}
	}
	// new ReplicaSet does not exist.
	return nil
}

// replicaSetsByCreationTimestamp sorts a list of ReplicaSet by creation timestamp, using their names as a tie breaker.
type replicaSetsByCreationTimestamp []*appsv1.ReplicaSet

func (o replicaSetsByCreationTimestamp) Len() int      { return len(o) }
func (o replicaSetsByCreationTimestamp) Swap(i, j int) { o[i], o[j] = o[j], o[i] }
func (o replicaSetsByCreationTimestamp) Less(i, j int) bool {
	if o[i].CreationTimestamp.Equal(&o[j].CreationTimestamp) {
		return o[i].Name < o[j].Name
	}
	return o[i].CreationTimestamp.Before(&o[j].CreationTimestamp)
}

// FindOldReplicaSets returns the old replica sets targeted by the given Deployment, with the given slice of RSes.
// Note that the first set of old replica sets doesn't include the ones with no pods, and the second set of old
// replica sets include all old replica sets.
func findOldReplicaSets(rsList []*appsv1.ReplicaSet, newRS *appsv1.ReplicaSet) ([]*appsv1.ReplicaSet, []*appsv1.ReplicaSet) {
	var requiredRSs []*appsv1.ReplicaSet
	var allRSs []*appsv1.ReplicaSet
	for _, rs := range rsList {
		// Filter out new replica set
		if newRS != nil && rs.UID == newRS.UID {
			continue
		}
		allRSs = append(allRSs, rs)
		if *(rs.Spec.Replicas) != 0 {
			requiredRSs = append(requiredRSs, rs)
		}
	}
	return requiredRSs, allRSs
}
