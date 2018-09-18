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

	v12 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"istio.io/istio/pkg/log"
)

var scope = log.RegisterScope("testframework", "General scope for the test framework", 0)

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

// FindPodBySelectors returns the first matching pod, given a namespace and a set of selectors.
func (a *Accessor) FindPodBySelectors(ns string, selectors ...string) (pod v12.Pod, err error) {
	s := strings.Join(selectors, ",")
	list, err := a.set.CoreV1().
		Pods(ns).
		List(v1.ListOptions{LabelSelector: s})

	if len(list.Items) == 0 {
		err = fmt.Errorf("no matching pod found for selectors: %q", s)
		return
	}

	if len(list.Items) > 1 {
		scope.Warnf("More than one pod found matching selectors: %s", s)
	}
	pod = list.Items[0]
	return
}

// WaitForPodBySelectors waits for the pod to appear that match the given namespace and selectors.
func (a *Accessor) WaitForPodBySelectors(ns string, selectors ...string) (pod v12.Pod, err error) {
	p, err := retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {

		s := strings.Join(selectors, ",")

		var list *v12.PodList
		if list, err = a.set.CoreV1().Pods(ns).List(v1.ListOptions{LabelSelector: s}); err != nil {
			return nil, false, err
		}

		if len(list.Items) > 0 {
			if len(list.Items) > 1 {
				scope.Warnf("More than one pod found matching selectors: %s", s)
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
	_, err := retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {

		pod, err := a.set.CoreV1().
			Pods(ns).
			Get(name, v1.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

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
	_, err := retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {

		pod, err := a.set.CoreV1().
			Pods(ns).
			Get(name, v1.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := pod.Status.ContainerStatuses[0].Ready

		return nil, ready, nil
	})

	return err
}

// WaitUntilDeploymentIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDeploymentIsReady(ns string, name string) error {
	_, err := retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {

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
	_, err := retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {

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

// GetService returns the service entry with the given name/namespace.
func (a *Accessor) GetService(ns string, name string) (*v12.Service, error) {
	svc, err := a.set.CoreV1().Services(ns).Get(name, v1.GetOptions{})
	return svc, err
}

// CreateNamespace with the given name. Also adds an "istio-testing" annotation.
func (a *Accessor) CreateNamespace(ns string, istioTestingAnnotation string) error {
	scope.Infof("Creating namespace: %s", ns)
	n := v12.Namespace{
		ObjectMeta: v1.ObjectMeta{
			Name: ns,
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
	scope.Infof("Deleting namespace: %s", ns)
	return a.set.CoreV1().Namespaces().Delete(ns, &v1.DeleteOptions{})
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func (a *Accessor) WaitForNamespaceDeletion(ns string) error {
	_, err := retry(defaultTimeout, defaultRetryWait, func() (interface{}, bool, error) {
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
