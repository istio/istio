//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package kube

import (
	"fmt"
	"strings"
	"time"

	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/test/framework/scopes"

	"istio.io/istio/pkg/test/util"
)

const (
	defaultTimeout     = time.Minute * 3
	defaultRetryPeriod = time.Second * 10
)

// Accessor is a helper for accessing Kubernetes programmatically. It bundles some of the high-level
// operations that is frequently used by the test framework.
type Accessor struct {
	set *kubernetes.Clientset
}

// NewAccessor returns a new instance of an accessor.
func NewAccessor(config *rest.Config) (*Accessor, error) {

	set, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return &Accessor{
		set: set,
	}, nil
}

// GetPods returns pods in the given namespace, based on the selectors. If no selectors are given, then
// all pods are returned.
func (a *Accessor) GetPods(namespace string, selectors ...string) ([]v12.Pod, error) {
	s := strings.Join(selectors, ",")
	list, err := a.set.CoreV1().
		Pods(namespace).
		List(v1.ListOptions{LabelSelector: s})

	if err != nil {
		return []v12.Pod{}, err
	}

	return list.Items, nil
}

// FindPodBySelectors returns the first matching pod, given a namespace and a set of selectors.
func (a *Accessor) FindPodBySelectors(namespace string, selectors ...string) (v12.Pod, error) {
	list, err := a.GetPods(namespace, selectors...)
	if err != nil {
		return v12.Pod{}, err
	}

	if len(list) == 0 {
		return v12.Pod{}, fmt.Errorf("no matching pod found for selectors: %v", selectors)
	}

	if len(list) > 1 {
		scopes.Framework.Warnf("More than one pod found matching selectors: %v", selectors)
	}

	return list[0], nil
}

// WaitForPodBySelectors waits for the pod to appear that match the given namespace and selectors.
func (a *Accessor) WaitForPodBySelectors(ns string, selectors ...string) (pod v12.Pod, err error) {
	p, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {

		s := strings.Join(selectors, ",")

		var list *v12.PodList
		if list, err = a.set.CoreV1().Pods(ns).List(v1.ListOptions{LabelSelector: s}); err != nil {
			return nil, false, err
		}

		if len(list.Items) > 0 {
			if len(list.Items) > 1 {
				scopes.Framework.Warnf("More than one pod found matching selectors: %s", s)
			}
			return &list.Items[0], true, nil
		}

		return nil, false, err
	})

	if p != nil {
		pod = *(p.(*v12.Pod))
	}

	return
}

// WaitUntilPodIsRunning waits until the pod with the name/namespace is in succeeded or in running state.
func (a *Accessor) WaitUntilPodIsRunning(ns string, name string) error {
	_, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {

		pod, err := a.set.CoreV1().
			Pods(ns).
			Get(name, v1.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		scopes.CI.Infof("  Checking pod state: %s/%s:\t %v", ns, name, pod.Status.Phase)

		switch pod.Status.Phase {
		case v12.PodSucceeded, v12.PodRunning:
			return nil, true, nil
		case v12.PodFailed:
			return nil, true, fmt.Errorf("pod found with selectors have failed:%s/%s", ns, name)
		}

		return nil, false, nil
	})

	return err
}

// WaitUntilPodIsReady waits until the pod with the name/namespace is in ready state.
func (a *Accessor) WaitUntilPodIsReady(ns string, name string) error {
	_, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {

		pod, err := a.set.CoreV1().
			Pods(ns).
			Get(name, v1.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := pod.Status.Phase == v12.PodRunning || pod.Status.Phase == v12.PodSucceeded

		return nil, ready, nil
	})

	return err
}

// WaitUntilDeploymentIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDeploymentIsReady(ns string, name string) error {
	_, err := util.Retry(util.DefaultRetryTimeout, util.DefaultRetryWait, func() (interface{}, bool, error) {

		deployment, err := a.set.ExtensionsV1beta1().
			Deployments(ns).
			Get(name, v1.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := deployment.Status.ReadyReplicas == deployment.Status.UnavailableReplicas+deployment.Status.AvailableReplicas

		return nil, ready, nil
	})

	return err
}

// WaitUntilDaemonSetIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDaemonSetIsReady(ns string, name string) error {
	_, err := util.Retry(util.DefaultRetryTimeout, util.DefaultRetryWait, func() (interface{}, bool, error) {

		daemonSet, err := a.set.ExtensionsV1beta1().
			DaemonSets(ns).
			Get(name, v1.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled

		return nil, ready, nil
	})

	return err
}

// WaitUntilPodsInNamespaceAreReady waits for pods to be become running/succeeded in a given namespace.
func (a *Accessor) WaitUntilPodsInNamespaceAreReady(ns string) error {
	scopes.CI.Infof("Starting wait for pods to be ready in namespace %s", ns)

	_, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {

		list, err := a.set.CoreV1().Pods(ns).List(v1.ListOptions{})
		if err != nil {
			scopes.Framework.Errorf("Error retrieving pods in namespace: %s: %v", ns, err)
			return nil, false, err
		}

		scopes.CI.Infof("  Pods in %q:", ns)
		for i, p := range list.Items {
			ready := p.Status.Phase == v12.PodRunning || p.Status.Phase == v12.PodSucceeded
			scopes.CI.Infof("  [%d] %s:\t %v (%v)", i, p.Name, p.Status.Phase, ready)
		}

		for _, p := range list.Items {
			ready := p.Status.Phase == v12.PodRunning || p.Status.Phase == v12.PodSucceeded
			if !ready {
				return nil, false, nil
			}
		}

		return nil, true, nil
	})

	return err
}

// GetService returns the service entry with the given name/namespace.
func (a *Accessor) GetService(ns string, name string) (*v12.Service, error) {
	svc, err := a.set.CoreV1().Services(ns).Get(name, v1.GetOptions{})
	return svc, err
}

// CreateNamespace with the given name. Also adds an "istio-testing" annotation.
func (a *Accessor) CreateNamespace(ns string, istioTestingAnnotation string) error {
	scopes.Framework.Debugf("Creating namespace: %s", ns)
	n := v12.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name:   ns,
			Labels: map[string]string{},
		},
	}
	if istioTestingAnnotation != "" {
		n.ObjectMeta.Labels["istio-testing"] = istioTestingAnnotation
	}

	_, err := a.set.CoreV1().Namespaces().Create(&n)
	return err
}

// NamespaceExists returns true if the given namespace exists.
func (a *Accessor) NamespaceExists(ns string) bool {
	allNs, err := a.set.CoreV1().Namespaces().List(v1.ListOptions{})
	if err != nil {
		return false
	}
	for _, n := range allNs.Items {
		if n.Name == ns {
			return true
		}
	}
	return false
}

// DeleteNamespace with the given name
func (a *Accessor) DeleteNamespace(ns string) error {
	scopes.Framework.Debugf("Deleting namespace: %s", ns)
	return a.set.CoreV1().Namespaces().Delete(ns, &v1.DeleteOptions{})
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func (a *Accessor) WaitForNamespaceDeletion(ns string) error {
	_, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {
		_, err2 := a.set.CoreV1().Namespaces().Get(ns, v1.GetOptions{})
		if err2 == nil {
			return nil, false, nil
		}

		if errors.IsNotFound(err2) {
			return nil, true, nil
		}

		return nil, true, err2
	})

	return err
}
