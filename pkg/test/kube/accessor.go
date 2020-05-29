//  Copyright Istio Authors
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
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ghodss/yaml"
	"github.com/hashicorp/go-multierror"
	kubeApiAdmissions "k8s.io/api/admissionregistration/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/api/autoscaling/v2beta1"
	kubeApiCore "k8s.io/api/core/v1"
	"k8s.io/api/policy/v1beta1"
	v1 "k8s.io/api/rbac/v1"
	kubeApiExt "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	kubeExtClient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/cli-runtime/pkg/printers"
	"k8s.io/cli-runtime/pkg/resource"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	kubeClientCore "k8s.io/client-go/kubernetes/typed/core/v1"
	_ "k8s.io/client-go/plugin/pkg/client/auth" // Needed for auth
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubectl/pkg/cmd/apply"
	"k8s.io/kubectl/pkg/cmd/delete"
	"k8s.io/kubectl/pkg/cmd/logs"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/test/util/yml"
)

const (
	workDirPrefix = "istio-kube-accessor-"
)

var (
	defaultRetryTimeout = retry.Timeout(time.Minute * 10)
	defaultRetryDelay   = retry.Delay(time.Second * 1)
)

// Accessor is a helper for accessing Kubernetes programmatically. It bundles some of the high-level
// operations that is frequently used by the test framework.
type Accessor struct {
	clientFactory util.Factory
	baseDir       string
	workDir       string
	workDirMutex  sync.Mutex
	clientSet     kubernetes.Interface
	extSet        *kubeExtClient.Clientset
	dynamicClient dynamic.Interface
}

// NewAccessor returns a new Accessor from a kube config file.
func NewAccessor(kubeConfig string, baseWorkDir string) (*Accessor, error) {
	clientFactory := istioKube.NewClientFactory(istioKube.BuildClientCmd(kubeConfig, ""), "")
	return NewAccessorForClientFactory(clientFactory, baseWorkDir)
}

// NewAccessorForClientFactory creates a new Accessor from a ClientConfig.
func NewAccessorForClientFactory(clientFactory util.Factory, baseWorkDir string) (*Accessor, error) {
	clientSet, err := clientFactory.KubernetesClientSet()
	if err != nil {
		return nil, err
	}

	restConfig, err := clientFactory.ToRESTConfig()
	if err != nil {
		return nil, err
	}

	extSet, err := kubeExtClient.NewForConfig(restConfig)
	if err != nil {
		return nil, err
	}

	dynamicClient, err := clientFactory.DynamicClient()
	if err != nil {
		return nil, err
	}

	return &Accessor{
		clientFactory: clientFactory,
		clientSet:     clientSet,
		extSet:        extSet,
		dynamicClient: dynamicClient,
		baseDir:       baseWorkDir,
	}, nil
}

// NewPortForwarder creates a new port forwarder.
func (a *Accessor) NewPortForwarder(pod kubeApiCore.Pod, localPort, remotePort uint16) (PortForwarder, error) {
	return newPortForwarder(a.clientFactory, pod, localPort, remotePort)
}

// GetPods returns pods in the given namespace, based on the selectors. If no selectors are given, then
// all pods are returned.
func (a *Accessor) GetPods(namespace string, selectors ...string) ([]kubeApiCore.Pod, error) {
	s := strings.Join(selectors, ",")
	list, err := a.clientSet.CoreV1().Pods(namespace).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: s})

	if err != nil {
		return []kubeApiCore.Pod{}, err
	}

	return list.Items, nil
}

func (a *Accessor) GetDeployments(namespace string, selectors ...string) ([]appsv1.Deployment, error) {
	s := strings.Join(selectors, ",")
	list, err := a.clientSet.AppsV1().Deployments(namespace).List(context.TODO(), kubeApiMeta.ListOptions{LabelSelector: s})

	if err != nil {
		return []appsv1.Deployment{}, err
	}

	return list.Items, nil
}

// GetEvents returns events in the given namespace, based on the involvedObject.
func (a *Accessor) GetEvents(namespace string, involvedObject string) ([]kubeApiCore.Event, error) {
	s := "involvedObject.name=" + involvedObject
	list, err := a.clientSet.CoreV1().Events(namespace).List(context.TODO(), kubeApiMeta.ListOptions{FieldSelector: s})

	if err != nil {
		return []kubeApiCore.Event{}, err
	}

	return list.Items, nil
}

// GetPod returns the pod with the given namespace and name.
func (a *Accessor) GetPod(namespace, name string) (kubeApiCore.Pod, error) {
	v, err := a.clientSet.CoreV1().
		Pods(namespace).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
	if err != nil {
		return kubeApiCore.Pod{}, err
	}
	return *v, nil
}

// DeletePod deletes the given pod.
func (a *Accessor) DeletePod(namespace, name string) error {
	return a.clientSet.CoreV1().Pods(namespace).Delete(context.TODO(), name, kubeApiMeta.DeleteOptions{})
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

// NewPodMustFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
// If no pods are found, an error is returned
func (a *Accessor) NewPodMustFetch(namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		pods, err := a.GetPods(namespace, selectors...)
		if err != nil {
			return nil, err
		}
		if len(pods) == 0 {
			return nil, fmt.Errorf("no pods found for %v", selectors)
		}
		return pods, nil
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

		fetched, err := a.CheckPodsAreReady(fetchFunc)
		if err != nil {
			return nil, false, err
		}
		pods = fetched
		return nil, true, nil
	}, newRetryOptions(opts...)...)

	return pods, err
}

// CheckPodsAreReady checks whether the pods that are selected by the given function is in ready state or not.
func (a *Accessor) CheckPodsAreReady(fetchFunc PodFetchFunc) ([]kubeApiCore.Pod, error) {
	scopes.CI.Infof("Checking pods ready...")

	fetched, err := fetchFunc()
	if err != nil {
		scopes.CI.Infof("Failed retrieving pods: %v", err)
		return nil, err
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
		return nil, err
	}

	return fetched, nil
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

// DeleteDeployment deletes the given deployment.
func (a *Accessor) DeleteDeployment(ns string, name string) error {
	return a.clientSet.AppsV1().Deployments(ns).Delete(context.TODO(), name, *deleteOptionsForeground())
}

// WaitUntilDeploymentIsReady waits until the deployment with the name/namespace is in ready state.
func (a *Accessor) WaitUntilDeploymentIsReady(ns string, name string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {

		deployment, err := a.clientSet.AppsV1().Deployments(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
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

		daemonSet, err := a.clientSet.AppsV1().DaemonSets(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
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
func (a *Accessor) WaitUntilServiceEndpointsAreReady(ns string, name string,
	opts ...retry.Option) (*kubeApiCore.Service, *kubeApiCore.Endpoints, error) {
	var service *kubeApiCore.Service
	var endpoints *kubeApiCore.Endpoints
	err := retry.UntilSuccess(func() error {

		s, err := a.GetService(ns, name)
		if err != nil {
			return err
		}

		eps, err := a.GetEndpoints(ns, name, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}
		if len(eps.Subsets) == 0 {
			return fmt.Errorf("%s/%v endpoint not ready: no subsets", ns, name)
		}

		for _, subset := range eps.Subsets {
			if len(subset.Addresses) > 0 && len(subset.NotReadyAddresses) == 0 {
				service = s
				endpoints = eps
				return nil
			}
		}
		return fmt.Errorf("%s/%v endpoint not ready: no ready addresses", ns, name)
	}, newRetryOptions(opts...)...)

	if err != nil {
		return nil, nil, err
	}

	return service, endpoints, nil
}

// DeleteMutatingWebhook deletes the mutating webhook with the given name.
func (a *Accessor) DeleteMutatingWebhook(name string) error {
	return a.clientSet.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Delete(context.TODO(),
		name, *deleteOptionsForeground())
}

// DeleteValidatingWebhook deletes the validating webhook with the given name.
func (a *Accessor) DeleteValidatingWebhook(name string) error {
	return a.clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Delete(context.TODO(),
		name, *deleteOptionsForeground())
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

// ValidatingWebhookConfigurationExists indicates whether a validating webhook with the given name exists.
func (a *Accessor) ValidatingWebhookConfigurationExists(name string) bool {
	_, err := a.clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(context.TODO(),
		name, kubeApiMeta.GetOptions{})
	return err == nil
}

// MutatingWebhookConfigurationExists indicates whether a mutating webhook with the given name exists.
func (a *Accessor) MutatingWebhookConfigurationExists(name string) bool {
	_, err := a.clientSet.AdmissionregistrationV1beta1().MutatingWebhookConfigurations().Get(context.TODO(),
		name, kubeApiMeta.GetOptions{})
	return err == nil
}

// GetValidatingWebhookConfiguration returns the specified ValidatingWebhookConfiguration.
func (a *Accessor) GetValidatingWebhookConfiguration(name string) (*kubeApiAdmissions.ValidatingWebhookConfiguration, error) {
	whc, err := a.clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Get(context.TODO(),
		name, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("could not get validating webhook config: %s", name)
	}
	return whc, nil
}

// UpdateValidatingWebhookConfiguration updates the specified ValidatingWebhookConfiguration.
func (a *Accessor) UpdateValidatingWebhookConfiguration(config *kubeApiAdmissions.ValidatingWebhookConfiguration) error {
	if _, err := a.clientSet.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations().Update(context.TODO(),
		config, kubeApiMeta.UpdateOptions{}); err != nil {
		return fmt.Errorf("could not update validating webhook config: %s", config.Name)
	}
	return nil
}

// GetCustomResourceDefinitions gets the CRDs
func (a *Accessor) GetCustomResourceDefinitions() ([]kubeApiExt.CustomResourceDefinition, error) {
	crd, err := a.extSet.ApiextensionsV1beta1().CustomResourceDefinitions().List(context.TODO(), kubeApiMeta.ListOptions{})
	if err != nil {
		return nil, err
	}
	return crd.Items, nil
}

// GetCustomResourceDefinition gets the CRD with the given name
func (a *Accessor) GetCustomResourceDefinition(name string) (*kubeApiExt.CustomResourceDefinition, error) {
	return a.extSet.ApiextensionsV1beta1().CustomResourceDefinitions().Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// DeleteCustomResourceDefinitions deletes the CRD with the given name.
func (a *Accessor) DeleteCustomResourceDefinitions(name string) error {
	return a.extSet.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(context.TODO(), name, *deleteOptionsForeground())
}

// GetPodDisruptionBudget gets the PodDisruptionBudget with the given name
func (a *Accessor) GetPodDisruptionBudget(ns, name string) (*v1beta1.PodDisruptionBudget, error) {
	return a.clientSet.PolicyV1beta1().PodDisruptionBudgets(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// GetHorizontalPodAutoscaler gets the HorizontalPodAutoscaler with the given name
func (a *Accessor) GetHorizontalPodAutoscaler(ns, name string) (*v2beta1.HorizontalPodAutoscaler, error) {
	return a.clientSet.AutoscalingV2beta1().HorizontalPodAutoscalers(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// GetService returns the service entry with the given name/namespace.
func (a *Accessor) GetService(ns string, name string) (*kubeApiCore.Service, error) {
	return a.clientSet.CoreV1().Services(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// GetDeployment returns the deployment with the given name/namespace.
func (a *Accessor) GetDeployment(ns string, name string) (*appsv1.Deployment, error) {
	return a.clientSet.AppsV1().Deployments(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// GetSecret returns secret resource with the given namespace.
func (a *Accessor) GetSecret(ns string) kubeClientCore.SecretInterface {
	return a.clientSet.CoreV1().Secrets(ns)
}

// GetConfigMap returns the config resource with the given name and namespace.
func (a *Accessor) GetConfigMap(name, ns string) (*kubeApiCore.ConfigMap, error) {
	return a.clientSet.CoreV1().ConfigMaps(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// DeleteConfigMap deletes the config resource with the given name and namespace.
func (a *Accessor) DeleteConfigMap(name, ns string) error {
	return a.clientSet.CoreV1().ConfigMaps(ns).Delete(context.TODO(), name, kubeApiMeta.DeleteOptions{})
}

// CreateSecret takes the representation of a secret and creates it in the given namespace.
// Returns an error if there is any.
func (a *Accessor) CreateSecret(namespace string, secret *kubeApiCore.Secret) (err error) {
	_, err = a.clientSet.CoreV1().Secrets(namespace).Create(context.TODO(), secret, kubeApiMeta.CreateOptions{})
	return err
}

// WaitForSecretToExist waits for the given secret up to the given waitTime.
func (a *Accessor) WaitForSecretToExist(namespace, name string, waitTime time.Duration) (*kubeApiCore.Secret, error) {
	secret := a.GetSecret(namespace)

	watch, err := secret.Watch(context.TODO(), kubeApiMeta.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set up watch for secret (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			secret := event.Object.(*kubeApiCore.Secret)
			if secret.GetName() == name {
				return secret, nil
			}
		case <-time.After(waitTime - time.Since(startTime)):
			return nil, fmt.Errorf("secret %v did not become existent within %v",
				name, waitTime)
		}
	}
}

// WaitForSecretToExistOrFail calls WaitForSecretToExist and fails the given test.Failer if an error occurs.
func (a *Accessor) WaitForSecretToExistOrFail(t test.Failer, namespace, name string,
	waitTime time.Duration) *kubeApiCore.Secret {
	t.Helper()
	s, err := a.WaitForSecretToExist(namespace, name, waitTime)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// DeleteSecret deletes secret by name in namespace.
func (a *Accessor) DeleteSecret(namespace, name string) (err error) {
	var immediate int64
	err = a.clientSet.CoreV1().Secrets(namespace).Delete(context.TODO(), name, kubeApiMeta.DeleteOptions{GracePeriodSeconds: &immediate})
	return err
}

func (a *Accessor) GetServiceAccount(namespace, name string) (*kubeApiCore.ServiceAccount, error) {
	return a.clientSet.CoreV1().ServiceAccounts(namespace).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
}

// GetKubernetesVersion returns the Kubernetes server version
func (a *Accessor) GetKubernetesVersion() (*version.Info, error) {
	return a.extSet.ServerVersion()
}

// GetEndpoints returns the endpoints for the given service.
func (a *Accessor) GetEndpoints(ns, service string, options kubeApiMeta.GetOptions) (*kubeApiCore.Endpoints, error) {
	return a.clientSet.CoreV1().Endpoints(ns).Get(context.TODO(), service, options)
}

// CreateNamespace with the given name. Also adds an "istio-testing" annotation.
func (a *Accessor) CreateNamespace(ns string, istioTestingAnnotation string) error {
	scopes.Framework.Debugf("Creating namespace: %s", ns)

	n := a.newNamespace(ns, istioTestingAnnotation)

	_, err := a.clientSet.CoreV1().Namespaces().Create(context.TODO(), &n, kubeApiMeta.CreateOptions{})
	return err
}

// CreateNamespaceWithLabels with the specified name, sidecar-injection behavior, and labels
func (a *Accessor) CreateNamespaceWithLabels(ns string, istioTestingAnnotation string, labels map[string]string) error {
	scopes.Framework.Debugf("Creating namespace %s ns with labels %v", ns, labels)

	n := a.newNamespaceWithLabels(ns, istioTestingAnnotation, labels)
	_, err := a.clientSet.CoreV1().Namespaces().Create(context.TODO(), &n, kubeApiMeta.CreateOptions{})
	return err
}

func (a *Accessor) newNamespace(ns string, istioTestingAnnotation string) kubeApiCore.Namespace {
	n := a.newNamespaceWithLabels(ns, istioTestingAnnotation, make(map[string]string))
	return n
}

func (a *Accessor) newNamespaceWithLabels(ns string, istioTestingAnnotation string, labels map[string]string) kubeApiCore.Namespace {
	n := kubeApiCore.Namespace{
		ObjectMeta: kubeApiMeta.ObjectMeta{
			Name:   ns,
			Labels: labels,
		},
	}
	if istioTestingAnnotation != "" {
		n.ObjectMeta.Labels["istio-testing"] = istioTestingAnnotation
	}
	return n
}

// NamespaceExists returns true if the given namespace exists.
func (a *Accessor) NamespaceExists(ns string) bool {
	allNs, err := a.clientSet.CoreV1().Namespaces().List(context.TODO(), kubeApiMeta.ListOptions{})
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
	return a.clientSet.CoreV1().Namespaces().Delete(context.TODO(), ns, *deleteOptionsForeground())
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func (a *Accessor) WaitForNamespaceDeletion(ns string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {
		_, err2 := a.clientSet.CoreV1().Namespaces().Get(context.TODO(), ns, kubeApiMeta.GetOptions{})
		if err2 == nil {
			return nil, false, nil
		}

		if errors.IsNotFound(err2) {
			return nil, true, nil
		}

		return nil, false, err2
	}, newRetryOptions(opts...)...)

	return err
}

// GetNamespace returns the K8s namespaceresource with the given name.
func (a *Accessor) GetNamespace(ns string) (*kubeApiCore.Namespace, error) {
	n, err := a.clientSet.CoreV1().Namespaces().Get(context.TODO(), ns, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, err
	}

	return n, nil
}

// DeleteClusterRole deletes a ClusterRole with the given name
func (a *Accessor) DeleteClusterRole(role string) error {
	scopes.Framework.Debugf("Deleting ClusterRole: %s", role)
	return a.clientSet.RbacV1().ClusterRoles().Delete(context.TODO(), role, *deleteOptionsForeground())
}

// GetClusterRole gets a ClusterRole with the given name
func (a *Accessor) GetClusterRole(role string) (*v1.ClusterRole, error) {
	return a.clientSet.RbacV1().ClusterRoles().Get(context.TODO(), role, kubeApiMeta.GetOptions{})
}

// GetClusterRoleBinding gets a ClusterRoleBinding with the given name
func (a *Accessor) GetClusterRoleBinding(role string) (*v1.ClusterRoleBinding, error) {
	return a.clientSet.RbacV1().ClusterRoleBindings().Get(context.TODO(), role, kubeApiMeta.GetOptions{})
}

// CreateNamespace with the given name. Also adds an "istio-testing" annotation.
func (a *Accessor) CreateServiceAccountToken(ns string, serviceAccount string) (string, error) {
	scopes.Framework.Debugf("Creating service account token for: %s/%s", ns, serviceAccount)

	token, err := a.clientSet.CoreV1().ServiceAccounts(ns).CreateToken(context.TODO(), serviceAccount, &authenticationv1.TokenRequest{
		Spec: authenticationv1.TokenRequestSpec{
			Audiences: []string{"istio-ca"},
		},
	}, kubeApiMeta.CreateOptions{})

	if err != nil {
		return "", err
	}
	return token.Status.Token, nil
}

// GetUnstructured returns an unstructured k8s resource object based on the provided schema, namespace, and name.
func (a *Accessor) GetUnstructured(gvr schema.GroupVersionResource, namespace, name string) (*unstructured.Unstructured, error) {
	u, err := a.dynamicClient.Resource(gvr).Namespace(namespace).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get resource %v of type %v: %v", name, gvr, err)
	}

	return u, nil
}

// DeleteUnstructured deletes an unstructured k8s resource object based on the provided schema, namespace, and name.
func (a *Accessor) DeleteUnstructured(gvr schema.GroupVersionResource, namespace, name string) error {
	if err := a.dynamicClient.Resource(gvr).Namespace(namespace).Delete(context.TODO(), name, kubeApiMeta.DeleteOptions{}); err != nil {
		return fmt.Errorf("failed to delete resource %v of type %v: %v", name, gvr, err)
	}
	return nil
}

// ApplyContents applies the given config contents using kubectl.
func (a *Accessor) ApplyContents(namespace string, contents string) ([]string, error) {
	return a.applyContents(namespace, contents, false)
}

// ApplyContentsDryRun applies the given config contents using kubectl with DryRun mode.
func (a *Accessor) ApplyContentsDryRun(namespace string, contents string) ([]string, error) {
	return a.applyContents(namespace, contents, true)
}

// Apply applies the config in the given filename using kubectl.
func (a *Accessor) Apply(namespace string, filename string) error {
	return a.apply(namespace, filename, false)
}

// ApplyDryRun applies the config in the given filename using kubectl with DryRun mode.
func (a *Accessor) ApplyDryRun(namespace string, filename string) error {
	return a.apply(namespace, filename, true)
}

// applyContents applies the given config contents using kubectl.
func (a *Accessor) applyContents(namespace string, contents string, dryRun bool) ([]string, error) {
	files, err := a.contentsToFileList(contents, "accessor_applyc")
	if err != nil {
		return nil, err
	}

	if err := a.applyInternal(namespace, files, dryRun); err != nil {
		return nil, err
	}

	return files, nil
}

// apply the config in the given filename using kubectl.
func (a *Accessor) apply(namespace string, filename string, dryRun bool) error {
	files, err := a.fileToFileList(filename)
	if err != nil {
		return err
	}

	return a.applyInternal(namespace, files, dryRun)
}

func (a *Accessor) applyInternal(namespace string, files []string, dryRun bool) error {
	for _, f := range removeEmptyFiles(files) {
		if err := a.applyFile(namespace, f, dryRun); err != nil {
			return err
		}
	}
	return nil
}

func (a *Accessor) applyFile(namespace string, file string, dryRun bool) error {
	if dryRun {
		scopes.CI.Infof("Applying YAML file (DryRun mode): %v", file)
	} else {
		scopes.CI.Infof("Applying YAML file: %v", file)
	}

	dynamicClient, err := a.clientFactory.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := a.clientFactory.ToDiscoveryClient()
	if err != nil {
		return err
	}

	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()
	opts := apply.NewApplyOptions(streams)
	opts.DynamicClient = dynamicClient
	opts.DryRunVerifier = resource.NewDryRunVerifier(dynamicClient, discoveryClient)
	opts.FieldManager = "kubectl"
	if dryRun {
		opts.DryRunStrategy = util.DryRunServer
	}

	// allow for a success message operation to be specified at print time
	opts.ToPrinter = func(operation string) (printers.ResourcePrinter, error) {
		opts.PrintFlags.NamePrintFlags.Operation = operation
		util.PrintFlagsWithDryRunStrategy(opts.PrintFlags, opts.DryRunStrategy)
		return opts.PrintFlags.ToPrinter()
	}

	if len(namespace) > 0 {
		opts.Namespace = namespace
		opts.EnforceNamespace = true
	} else {
		var err error
		opts.Namespace, opts.EnforceNamespace, err = a.clientFactory.ToRawKubeConfigLoader().Namespace()
		if err != nil {
			return err
		}
	}

	opts.DeleteFlags.FileNameFlags.Filenames = &[]string{file}
	opts.DeleteOptions = &delete.DeleteOptions{
		DynamicClient:   dynamicClient,
		IOStreams:       streams,
		FilenameOptions: opts.DeleteFlags.FileNameFlags.ToOptions(),
	}

	opts.OpenAPISchema, _ = a.clientFactory.OpenAPISchema()

	opts.Validator, err = a.clientFactory.Validator(true)
	if err != nil {
		return err
	}
	opts.Builder = a.clientFactory.NewBuilder()
	opts.Mapper, err = a.clientFactory.ToRESTMapper()
	if err != nil {
		return err
	}

	opts.PostProcessorFn = opts.PrintAndPrunePostProcessor()

	if err := opts.Run(); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		scopes.CI.Infof("(FAILED) Executing kubectl apply: %s (err: %v): %s", file, err, s)
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
}

// DeleteContents deletes the given config contents using kubectl.
func (a *Accessor) DeleteContents(namespace string, contents string) error {
	files, err := a.contentsToFileList(contents, "accessor_deletec")
	if err != nil {
		return err
	}

	return a.deleteInternal(namespace, files)
}

// Delete the config in the given filename using kubectl.
func (a *Accessor) Delete(namespace string, filename string) error {
	files, err := a.fileToFileList(filename)
	if err != nil {
		return err
	}

	return a.deleteInternal(namespace, files)
}

func (a *Accessor) deleteInternal(namespace string, files []string) (err error) {
	for _, f := range removeEmptyFiles(files) {
		err = multierror.Append(err, a.deleteFile(namespace, f)).ErrorOrNil()
	}
	return err
}

func (a *Accessor) deleteFile(namespace string, file string) error {
	scopes.CI.Infof("Deleting YAML file: %v", file)
	// Create the options.
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()

	cmdNamespace, enforceNamespace, err := a.clientFactory.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	if len(namespace) > 0 {
		cmdNamespace = namespace
		enforceNamespace = true
	}

	fileOpts := resource.FilenameOptions{
		Filenames: []string{file},
	}

	dynamicClient, err := a.clientFactory.DynamicClient()
	if err != nil {
		return err
	}
	discoveryClient, err := a.clientFactory.ToDiscoveryClient()
	if err != nil {
		return err
	}
	opts := delete.DeleteOptions{
		FilenameOptions:  fileOpts,
		Cascade:          true,
		GracePeriod:      -1,
		IgnoreNotFound:   true,
		WaitForDeletion:  true,
		WarnClusterScope: enforceNamespace,
		DynamicClient:    dynamicClient,
		DryRunVerifier:   resource.NewDryRunVerifier(dynamicClient, discoveryClient),
		IOStreams:        streams,
	}

	r := a.clientFactory.NewBuilder().
		Unstructured().
		ContinueOnError().
		NamespaceParam(cmdNamespace).DefaultNamespace().
		FilenameParam(enforceNamespace, &fileOpts).
		LabelSelectorParam(opts.LabelSelector).
		FieldSelectorParam(opts.FieldSelector).
		SelectAllParam(opts.DeleteAll).
		AllNamespaces(opts.DeleteAllNamespaces).
		Flatten().
		Do()
	err = r.Err()
	if err != nil {
		return err
	}
	opts.Result = r

	opts.Mapper, err = a.clientFactory.ToRESTMapper()
	if err != nil {
		return err
	}

	if err := opts.RunDelete(a.clientFactory); err != nil {
		// Concatenate the stdout and stderr
		s := stdout.String() + stderr.String()
		scopes.CI.Infof("(FAILED) Executing kubectl delete: %s (err: %v): %s", file, err, s)
		return fmt.Errorf("%v: %s", err, s)
	}
	return nil
}

// Logs calls the logs command for the specified pod, with -c, if container is specified.
func (a *Accessor) Logs(namespace string, pod string, container string, previousLog bool) (string, error) {
	streams, _, stdout, stderr := genericclioptions.NewTestIOStreams()
	containerNameSpecified := len(container) > 0
	opts := logs.NewLogsOptions(streams, !containerNameSpecified)
	opts.Container = container
	opts.ContainerNameSpecified = containerNameSpecified
	opts.Previous = previousLog
	opts.ResourceArg = pod
	opts.Resources = []string{pod}
	opts.Namespace = namespace
	opts.ConsumeRequestFn = logs.DefaultConsumeRequest

	var err error
	opts.Options, err = opts.ToLogOptions()
	if err != nil {
		return "", err
	}

	opts.RESTClientGetter = a.clientFactory
	opts.LogsForObject = polymorphichelpers.LogsForObjectFn

	builder := a.clientFactory.NewBuilder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(opts.Namespace).DefaultNamespace().
		SingleResourceType().
		ResourceNames("pods", pod)

	infos, err := builder.Do().Infos()
	if err != nil {
		return "", err
	}
	if len(infos) != 1 {
		return "", fmt.Errorf("expected a resource")
	}
	opts.Object = infos[0].Object

	err = opts.RunLogs()
	s := stdout.String() + stderr.String()

	if err != nil {
		// Concatenate the stdout and stderr
		scopes.CI.Infof("(FAILED) Executing kubectl logs (ns: %s, pod: %s, container: %s) (err: %v): %s",
			namespace, pod, container, err, s)
		return s, fmt.Errorf("%v: %s", err, s)
	}

	return s, nil
}

// Exec executes the provided command on the specified pod/container.
func (a *Accessor) Exec(namespace, podName, containerName, command string) (string, error) {
	pod, err := a.clientSet.CoreV1().Pods(namespace).Get(context.TODO(),
		podName, kubeApiMeta.GetOptions{})
	if err != nil {
		return "", err
	}

	if pod.Status.Phase == kubeApiCore.PodSucceeded || pod.Status.Phase == kubeApiCore.PodFailed {
		return "", fmt.Errorf("cannot exec into a container in a completed pod; current phase is %s", pod.Status.Phase)
	}

	if len(containerName) == 0 {
		containerName = pod.Spec.Containers[0].Name
	}

	commandFields := strings.Fields(command)

	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	restConfig, err := a.clientFactory.ToRESTConfig()
	if err != nil {
		return "", err
	}

	restClient, err := a.clientFactory.RESTClient()
	if err != nil {
		return "", err
	}

	request := restClient.
		Post().
		Resource("pods").
		Namespace(namespace).
		Name(podName).
		SubResource("exec").
		VersionedParams(&kubeApiCore.PodExecOptions{
			Container: containerName,
			Command:   commandFields,
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)
	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", request.URL())
	if err != nil {
		return "", err
	}
	err = exec.Stream(remotecommand.StreamOptions{
		Stdout: stdout,
		Stderr: stderr,
	})

	combined := stdout.String() + stderr.String()
	if err != nil {
		scopes.CI.Infof("(FAILED) Executing kubectl exec (ns: %s, pod: %s, container: %s, command: %s: %v: %s",
			namespace, podName, containerName, command, err, combined)
		return combined, err
	}

	return combined, nil
}

// getWorkDir lazy-creates the working directory for the accessor.
func (a *Accessor) getWorkDir() (string, error) {
	a.workDirMutex.Lock()
	defer a.workDirMutex.Unlock()

	workDir := a.workDir
	if workDir == "" {
		var err error
		if workDir, err = ioutil.TempDir(a.baseDir, workDirPrefix); err != nil {
			return "", err
		}
		a.workDir = workDir
	}
	return workDir, nil
}

func (a *Accessor) fileToFileList(filename string) ([]string, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	files, err := a.splitContentsToFiles(string(content), filenameWithoutExtension(filename))
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		files = append(files, filename)
	}

	return files, nil
}

func filenameWithoutExtension(fullPath string) string {
	_, f := filepath.Split(fullPath)
	return strings.TrimSuffix(f, filepath.Ext(fullPath))
}

func (a *Accessor) contentsToFileList(contents, filenamePrefix string) ([]string, error) {
	files, err := a.splitContentsToFiles(contents, filenamePrefix)
	if err != nil {
		return nil, err
	}

	if len(files) == 0 {
		f, err := a.writeContentsToTempFile(contents)
		if err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, nil
}

func (a *Accessor) writeContentsToTempFile(contents string) (filename string, err error) {
	defer func() {
		if err != nil && filename != "" {
			_ = os.Remove(filename)
			filename = ""
		}
	}()

	var workdir string
	workdir, err = a.getWorkDir()
	if err != nil {
		return
	}

	var f *os.File
	f, err = ioutil.TempFile(workdir, "accessor_")
	if err != nil {
		return
	}
	filename = f.Name()

	_, err = f.WriteString(contents)
	return
}

func (a *Accessor) splitContentsToFiles(content, filenamePrefix string) ([]string, error) {
	cfgs := yml.SplitString(content)

	namespacesAndCrds := &yamlDoc{
		docType: namespacesAndCRDs,
	}
	misc := &yamlDoc{
		docType: misc,
	}
	for _, cfg := range cfgs {
		var typeMeta kubeApiMeta.TypeMeta
		if e := yaml.Unmarshal([]byte(cfg), &typeMeta); e != nil {
			// Ignore invalid parts. This most commonly happens when it's empty or contains only comments.
			continue
		}

		switch typeMeta.Kind {
		case "Namespace":
			namespacesAndCrds.append(cfg)
		case "CustomResourceDefinition":
			namespacesAndCrds.append(cfg)
		default:
			misc.append(cfg)
		}
	}

	// If all elements were put into a single doc just return an empty list, indicating that the original
	// content should be used.
	docs := []*yamlDoc{namespacesAndCrds, misc}
	for _, doc := range docs {
		if len(doc.content) == 0 {
			return make([]string, 0), nil
		}
	}

	filesToApply := make([]string, 0, len(docs))
	for _, doc := range docs {
		workDir, err := a.getWorkDir()
		if err != nil {
			return nil, err
		}

		tfile, err := doc.toTempFile(workDir, filenamePrefix)
		if err != nil {
			return nil, err
		}
		filesToApply = append(filesToApply, tfile)
	}
	return filesToApply, nil
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
		if len(pod.Status.Conditions) > 0 {
			for _, condition := range pod.Status.Conditions {
				if condition.Type == kubeApiCore.PodReady && condition.Status != kubeApiCore.ConditionTrue {
					return fmt.Errorf("pod not ready, condition message: %v", condition.Message)
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("%s", pod.Status.Phase)
	}
}

func deleteOptionsForeground() *kubeApiMeta.DeleteOptions {
	propagationPolicy := kubeApiMeta.DeletePropagationForeground
	gracePeriod := int64(0)
	return &kubeApiMeta.DeleteOptions{
		PropagationPolicy:  &propagationPolicy,
		GracePeriodSeconds: &gracePeriod,
	}
}

func newRetryOptions(opts ...retry.Option) []retry.Option {
	out := make([]retry.Option, 0, 2+len(opts))
	out = append(out, defaultRetryTimeout, defaultRetryDelay)
	out = append(out, opts...)
	return out
}

func removeEmptyFiles(files []string) []string {
	out := make([]string, 0, len(files))
	for _, f := range files {
		if !isEmptyFile(f) {
			out = append(out, f)
		}
	}
	return out
}

func isEmptyFile(f string) bool {
	fileInfo, err := os.Stat(f)
	if err != nil {
		scopes.CI.Warnf("Error stating YAML file %s: %v", f, err)
		return true
	}
	if fileInfo.Size() == 0 {
		scopes.CI.Warnf("Unable to process empty YAML file: %s", f)
		return true
	}
	return false
}

type docType string

const (
	namespacesAndCRDs docType = "namespaces_and_crds"
	misc              docType = "misc"
)

type yamlDoc struct {
	content string
	docType docType
}

func (d *yamlDoc) append(c string) {
	d.content = yml.JoinString(d.content, c)
}

func (d *yamlDoc) toTempFile(workDir, fileNamePrefix string) (string, error) {
	f, err := ioutil.TempFile(workDir, fmt.Sprintf("%s_%s.yaml", fileNamePrefix, d.docType))
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	name := f.Name()

	_, err = f.WriteString(d.content)
	if err != nil {
		return "", err
	}
	return name, nil
}
