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
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"strings"
	"time"

	kubeApiCore "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeClient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	kubeClientCore "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"

	istioKube "istio.io/istio/pkg/kube"
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
	restConfig *rest.Config
	ctl        *kubectl
	set        *kubeClient.Clientset
}

// NewAccessor returns a new instance of an accessor.
func NewAccessor(kubeConfig string) (*Accessor, error) {
	restConfig, err := istioKube.BuildClientConfig(kubeConfig, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create rest config. %v", err)
	}
	restConfig.APIPath = "/api"
	restConfig.GroupVersion = &kubeApiCore.SchemeGroupVersion
	restConfig.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}

	set, err := kubeClient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &Accessor{
		restConfig: restConfig,
		ctl:        &kubectl{kubeConfig},
		set:        set,
	}, nil
}

// NewPortForwarder creates a new port forwarder.
func (a *Accessor) NewPortForwarder(options *PodSelectOptions, localPort, remotePort uint16) (PortForwarder, error) {
	return newPortForwarder(a.restConfig, options, localPort, remotePort)
}

// GetPods returns pods in the given namespace, based on the selectors. If no selectors are given, then
// all pods are returned.
func (a *Accessor) GetPods(namespace string, selectors ...string) ([]kubeApiCore.Pod, error) {
	s := strings.Join(selectors, ",")
	list, err := a.set.CoreV1().
		Pods(namespace).
		List(kubeApiMeta.ListOptions{LabelSelector: s})

	if err != nil {
		return []kubeApiCore.Pod{}, err
	}

	return list.Items, nil
}

// FindPodBySelectors returns the first matching pod, given a namespace and a set of selectors.
func (a *Accessor) FindPodBySelectors(namespace string, selectors ...string) (kubeApiCore.Pod, error) {
	list, err := a.GetPods(namespace, selectors...)
	if err != nil {
		return kubeApiCore.Pod{}, err
	}

	if len(list) == 0 {
		return kubeApiCore.Pod{}, fmt.Errorf("no matching pod found for selectors: %v", selectors)
	}

	if len(list) > 1 {
		scopes.Framework.Warnf("More than one pod found matching selectors: %v", selectors)
	}

	return list[0], nil
}

// WaitForPodBySelectors waits for the pod to appear that match the given namespace and selectors.
func (a *Accessor) WaitForPodBySelectors(ns string, selectors ...string) (pod kubeApiCore.Pod, err error) {
	p, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {

		s := strings.Join(selectors, ",")

		var list *kubeApiCore.PodList
		if list, err = a.set.CoreV1().Pods(ns).List(kubeApiMeta.ListOptions{LabelSelector: s}); err != nil {
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
		pod = *(p.(*kubeApiCore.Pod))
	}

	return
}

// WaitUntilPodIsRunning waits until the pod with the name/namespace is in succeeded or in running state.
func (a *Accessor) WaitUntilPodIsRunning(ns string, name string) error {
	_, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {

		pod, err := a.set.CoreV1().
			Pods(ns).
			Get(name, kubeApiMeta.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		scopes.CI.Infof("  Checking pod state: %s/%s:\t %v", ns, name, pod.Status.Phase)

		switch pod.Status.Phase {
		case kubeApiCore.PodSucceeded:
			return nil, true, nil
		case kubeApiCore.PodRunning:
			// Wait until all containers are ready.
			for _, containerStatus := range pod.Status.ContainerStatuses {
				if !containerStatus.Ready {
					return nil, false, fmt.Errorf("pod %s running, but container %s not ready", name, containerStatus.ContainerID)
				}
			}
			return nil, true, nil
		case kubeApiCore.PodFailed:
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
			Get(name, kubeApiMeta.GetOptions{})

		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := pod.Status.Phase == kubeApiCore.PodRunning || pod.Status.Phase == kubeApiCore.PodSucceeded

		return nil, ready, nil
	})

	return err
}

// WaitUntilDeploymentIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDeploymentIsReady(ns string, name string) error {
	_, err := util.Retry(util.DefaultRetryTimeout, util.DefaultRetryWait, func() (interface{}, bool, error) {

		deployment, err := a.set.ExtensionsV1beta1().
			Deployments(ns).
			Get(name, kubeApiMeta.GetOptions{})

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
			Get(name, kubeApiMeta.GetOptions{})

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

		list, err := a.set.CoreV1().Pods(ns).List(kubeApiMeta.ListOptions{})
		if err != nil {
			scopes.Framework.Errorf("Error retrieving pods in namespace: %s: %v", ns, err)
			return nil, false, err
		}

		scopes.CI.Infof("  Pods in %q:", ns)
		for i, p := range list.Items {
			ready := p.Status.Phase == kubeApiCore.PodRunning || p.Status.Phase == kubeApiCore.PodSucceeded
			scopes.CI.Infof("  [%d] %s:\t %v (%v)", i, p.Name, p.Status.Phase, ready)
		}

		for _, p := range list.Items {
			ready := p.Status.Phase == kubeApiCore.PodRunning || p.Status.Phase == kubeApiCore.PodSucceeded
			if !ready {
				return nil, false, nil
			}
		}

		return nil, true, nil
	})

	return err
}

// GetService returns the service entry with the given name/namespace.
func (a *Accessor) GetService(ns string, name string) (*kubeApiCore.Service, error) {
	svc, err := a.set.CoreV1().Services(ns).Get(name, kubeApiMeta.GetOptions{})
	return svc, err
}

// GetSecret returns secret resource with the given namespace.
func (a *Accessor) GetSecret(ns string) kubeClientCore.SecretInterface {
	return a.set.CoreV1().Secrets(ns)
}

// CreateNamespace with the given name. Also adds an "istio-testing" annotation.
func (a *Accessor) CreateNamespace(ns string, istioTestingAnnotation string, injectionEnabled bool) error {
	scopes.Framework.Debugf("Creating namespace: %s", ns)
	n := kubeApiCore.Namespace{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:   ns,
			Labels: map[string]string{},
		},
	}
	if istioTestingAnnotation != "" {
		n.ObjectMeta.Labels["istio-testing"] = istioTestingAnnotation
	}
	if injectionEnabled {
		n.ObjectMeta.Labels["istio-injection"] = "enabled"
	}

	_, err := a.set.CoreV1().Namespaces().Create(&n)
	return err
}

// NamespaceExists returns true if the given namespace exists.
func (a *Accessor) NamespaceExists(ns string) bool {
	allNs, err := a.set.CoreV1().Namespaces().List(kubeApiMeta.ListOptions{})
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
	return a.set.CoreV1().Namespaces().Delete(ns, &kubeApiMeta.DeleteOptions{})
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func (a *Accessor) WaitForNamespaceDeletion(ns string) error {
	_, err := util.Retry(defaultTimeout, defaultRetryPeriod, func() (interface{}, bool, error) {
		_, err2 := a.set.CoreV1().Namespaces().Get(ns, kubeApiMeta.GetOptions{})
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

// ApplyContents applies the given config contents using kubectl.
func (a *Accessor) ApplyContents(namespace string, contents string) error {
	return a.ctl.applyContents(namespace, contents)
}

// Apply the config in the given filename using kubectl.
func (a *Accessor) Apply(namespace string, filename string) error {
	return a.ctl.apply(namespace, filename)
}

// DeleteContents deletes the given config contents using kubectl.
func (a *Accessor) DeleteContents(namespace string, contents string) error {
	return a.ctl.deleteContents(namespace, contents)
}

// Delete the config in the given filename using kubectl.
func (a *Accessor) Delete(namespace string, filename string) error {
	return a.ctl.delete(namespace, filename)
}

// Logs calls the logs command for the specified pod, with -c, if container is specified.
func (a *Accessor) Logs(namespace string, pod string, container string) (string, error) {
	return a.ctl.logs(namespace, pod, container)
}

// Exec executes the provided command on the specified pod/container.
func (a *Accessor) Exec(namespace, pod, container, command string) (string, error) {
	return a.ctl.exec(namespace, pod, container, command)
}
