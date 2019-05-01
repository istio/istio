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

	"github.com/hashicorp/go-multierror"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"

	kubeApiCore "k8s.io/api/core/v1"
	kubeApiExt "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	kubeClient "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	kubeClientCore "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Needed for auth
	"k8s.io/client-go/rest"
)

const (
	workDirPrefix = "istio-kube-accessor-"
)

var (
	defaultRetryTimeout = retry.Timeout(time.Minute * 10)
	defaultRetryDelay   = retry.Delay(time.Second * 10)
)

// Accessor is a helper for accessing Kubernetes programmatically. It bundles some of the high-level
// operations that is frequently used by the test framework.
type Accessor struct {
	restConfig *rest.Config
	ctl        *kubectl
	set        *kubeClient.Clientset
	extSet     *kubeExtClient.Clientset
}

// NewAccessor returns a new instance of an accessor.
func NewAccessor(kubeConfig string, baseWorkDir string) (*Accessor, error) {
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

	extSet, err := kubeExtClient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	return &Accessor{
		restConfig: restConfig,
		ctl: &kubectl{
			kubeConfig: kubeConfig,
			baseDir:    baseWorkDir,
		},
		set:    set,
		extSet: extSet,
	}, nil
}

// NewPortForwarder creates a new port forwarder.
func (a *Accessor) NewPortForwarder(pod kubeApiCore.Pod, localPort, remotePort uint16) (PortForwarder, error) {
	return newPortForwarder(a.restConfig, pod, localPort, remotePort)
}

// GetPods returns pods in the given namespace, based on the selectors. If no selectors are given, then
// all pods are returned.
func (a *Accessor) GetPods(namespace string, selectors ...string) ([]kubeApiCore.Pod, error) {
	s := strings.Join(selectors, ",")
	list, err := a.set.CoreV1().Pods(namespace).List(kubeApiMeta.ListOptions{LabelSelector: s})

	if err != nil {
		return []kubeApiCore.Pod{}, err
	}

	return list.Items, nil
}

// GetEvents returns events in the given namespace, based on the involvedObject.
func (a *Accessor) GetEvents(namespace string, involvedObject string) ([]kubeApiCore.Event, error) {
	s := "involvedObject.name=" + involvedObject
	list, err := a.set.CoreV1().Events(namespace).List(kubeApiMeta.ListOptions{FieldSelector: s})

	if err != nil {
		return []kubeApiCore.Event{}, err
	}

	return list.Items, nil
}

// GetPod returns the pod with the given namespace and name.
func (a *Accessor) GetPod(namespace, name string) (kubeApiCore.Pod, error) {
	v, err := a.set.CoreV1().
		Pods(namespace).Get(name, kubeApiMeta.GetOptions{})
	if err != nil {
		return kubeApiCore.Pod{}, err
	}
	return *v, nil
}

// DeletePod deletes the given pod.
func (a *Accessor) DeletePod(namespace, name string) error {
	return a.set.CoreV1().Pods(namespace).Delete(name, &kubeApiMeta.DeleteOptions{})
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

// PodFetchFunc fetches pods from the Accessor.
type PodFetchFunc func() ([]kubeApiCore.Pod, error)

// NewPodFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
func (a *Accessor) NewPodFetch(namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		return a.GetPods(namespace, selectors...)
	}
}

// NewSinglePodFetch creates a new PodFetchFunction that fetches a single pod matching the given label selectors.
func (a *Accessor) NewSinglePodFetch(namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		pod, err := a.FindPodBySelectors(namespace, selectors...)
		if err != nil {
			return nil, err
		}
		return []kubeApiCore.Pod{pod}, nil
	}
}

// WaitUntilPodsAreReady waits until the pod with the name/namespace is in ready state.
func (a *Accessor) WaitUntilPodsAreReady(fetchFunc PodFetchFunc, opts ...retry.Option) ([]kubeApiCore.Pod, error) {
	var pods []kubeApiCore.Pod
	_, err := retry.Do(func() (interface{}, bool, error) {

		scopes.CI.Infof("Checking pods ready...")

		fetched, err := fetchFunc()
		if err != nil {
			scopes.CI.Infof("Failed retrieving pods: %v", err)
			return nil, false, err
		}

		for i, p := range fetched {
			msg := "Ready"
			if e := CheckPodReady(&p); e != nil {
				msg = e.Error()
				err = multierror.Append(err, fmt.Errorf("%s/%s: %s", p.Namespace, p.Name, msg))
			}
			scopes.CI.Infof("  [%2d] %45s %15s (%v)", i, p.Name, p.Status.Phase, msg)
		}

		if err != nil {
			return nil, false, err
		}
		pods = fetched
		return nil, true, nil
	}, newRetryOptions(opts...)...)

	return pods, err
}

// WaitUntilPodsAreDeleted waits until the pod with the name/namespace no longer exist.
func (a *Accessor) WaitUntilPodsAreDeleted(fetchFunc PodFetchFunc, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {

		pods, err := fetchFunc()
		if err != nil {
			scopes.CI.Infof("Failed retrieving pods: %v", err)
			return nil, false, err
		}

		if len(pods) == 0 {
			// All pods have been deleted.
			return nil, true, nil
		}

		return nil, false, fmt.Errorf("failed waiting to delete pod %s/%s", pods[0].Namespace, pods[0].Name)
	}, newRetryOptions(opts...)...)

	return err
}

// WaitUntilDeploymentIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDeploymentIsReady(ns string, name string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {

		deployment, err := a.set.ExtensionsV1beta1().Deployments(ns).Get(name, kubeApiMeta.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := deployment.Status.ReadyReplicas == deployment.Status.UnavailableReplicas+deployment.Status.AvailableReplicas

		return nil, ready, nil
	}, newRetryOptions(opts...)...)

	return err
}

// WaitUntilDaemonSetIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDaemonSetIsReady(ns string, name string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {

		daemonSet, err := a.set.ExtensionsV1beta1().DaemonSets(ns).Get(name, kubeApiMeta.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return nil, true, err
			}
		}

		ready := daemonSet.Status.NumberReady == daemonSet.Status.DesiredNumberScheduled

		return nil, ready, nil
	}, newRetryOptions(opts...)...)

	return err
}

// WaitUntilServiceEndpointsAreReady will wait until the service with the given name/namespace is present, and have at least
// one usable endpoint.
func (a *Accessor) WaitUntilServiceEndpointsAreReady(ns string, name string, opts ...retry.Option) (*kubeApiCore.Service, error) {
	var service *kubeApiCore.Service
	err := retry.UntilSuccess(func() error {

		s, err := a.GetService(ns, name)
		if err != nil {
			return err
		}

		endpoints, err := a.GetEndpoints(ns, name, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}
		if len(endpoints.Subsets) == 0 {
			return fmt.Errorf("%s/%v endpoint not ready: no subsets", ns, name)
		}

		for _, subset := range endpoints.Subsets {
			if len(subset.Addresses) > 0 && len(subset.NotReadyAddresses) == 0 {
				service = s
				return nil
			}
		}
		return fmt.Errorf("%s/%v endpoint not ready: no ready addresses", ns, name)
	}, opts...)

	if err != nil {
		return nil, err
	}

	return service, nil
}

// DeleteValidatingWebhook deletes the validating webhook with the given name.
func (a *Accessor) DeleteValidatingWebhook(name string) error {
	return a.set.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Delete(name, deleteOptionsForeground())
}

// WaitForValidatingWebhookDeletion waits for the validating webhook with the given name to be garbage collected by kubernetes.
func (a *Accessor) WaitForValidatingWebhookDeletion(name string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {
		if a.ValidatingWebhookConfigurationExists(name) {
			return nil, false, fmt.Errorf("validating webhook not deleted: %s", name)
		}

		// It no longer exists ... success.
		return nil, true, nil
	}, newRetryOptions(opts...)...)

	return err
}

// ValidatingWebhookConfigurationExists indicates whether a mutating validating with the given name exists.
func (a *Accessor) ValidatingWebhookConfigurationExists(name string) bool {
	_, err := a.set.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(name, kubeApiMeta.GetOptions{})
	return err == nil
}

// GetCustomResourceDefinitions gets the CRDs
func (a *Accessor) GetCustomResourceDefinitions() ([]kubeApiExt.CustomResourceDefinition, error) {
	crd, err := a.extSet.ApiextensionsV1beta1().CustomResourceDefinitions().List(kubeApiMeta.ListOptions{})
	if err != nil {
		return nil, err
	}
	return crd.Items, nil
}

// DeleteCustomResourceDefinitions deletes the CRD with the given name.
func (a *Accessor) DeleteCustomResourceDefinitions(name string) error {
	return a.extSet.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(name, deleteOptionsForeground())
}

// GetService returns the service entry with the given name/namespace.
func (a *Accessor) GetService(ns string, name string) (*kubeApiCore.Service, error) {
	return a.set.CoreV1().Services(ns).Get(name, kubeApiMeta.GetOptions{})
}

// GetSecret returns secret resource with the given namespace.
func (a *Accessor) GetSecret(ns string) kubeClientCore.SecretInterface {
	return a.set.CoreV1().Secrets(ns)
}

// GetEndpoints returns the endpoints for the given service.
func (a *Accessor) GetEndpoints(ns, service string, options kubeApiMeta.GetOptions) (*kubeApiCore.Endpoints, error) {
	return a.set.CoreV1().Endpoints(ns).Get(service, options)
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
	return a.set.CoreV1().Namespaces().Delete(ns, deleteOptionsForeground())
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func (a *Accessor) WaitForNamespaceDeletion(ns string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {
		_, err2 := a.set.CoreV1().Namespaces().Get(ns, kubeApiMeta.GetOptions{})
		if err2 == nil {
			return nil, false, nil
		}

		if errors.IsNotFound(err2) {
			return nil, true, nil
		}

		return nil, true, err2
	}, newRetryOptions(opts...)...)

	return err
}

// GetNamespace returns the K8s namespaceresource with the given name.
func (a *Accessor) GetNamespace(ns string) (*kubeApiCore.Namespace, error) {
	n, err := a.set.CoreV1().Namespaces().Get(ns, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, err
	}

	return n, nil
}

// ApplyContents applies the given config contents using kubectl.
func (a *Accessor) ApplyContents(namespace string, contents string) ([]string, error) {
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

// CheckPodReady returns nil if the given pod and all of its containers are ready.
func CheckPodReady(pod *kubeApiCore.Pod) error {
	switch pod.Status.Phase {
	case kubeApiCore.PodSucceeded:
		return nil
	case kubeApiCore.PodRunning:
		// Wait until all containers are ready.
		for _, containerStatus := range pod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				return fmt.Errorf("container not ready: '%s'", containerStatus.Name)
			}
		}
		return nil
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

func deleteOptionsForeground() *kubeApiMeta.DeleteOptions {
	propagationPolicy := kubeApiMeta.DeletePropagationForeground
	return &kubeApiMeta.DeleteOptions{
		PropagationPolicy: &propagationPolicy,
	}
}

func newRetryOptions(opts ...retry.Option) []retry.Option {
	out := make([]retry.Option, 0, 2+len(opts))
	out = append(out, defaultRetryTimeout, defaultRetryDelay)
	out = append(out, opts...)
	return out
}
