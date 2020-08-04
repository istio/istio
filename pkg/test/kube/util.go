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
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/go-multierror"
	kubeApiCore "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	kubeApiMeta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	defaultRetryTimeout = retry.Timeout(time.Minute * 10)
	defaultRetryDelay   = retry.Delay(time.Second * 1)
)

// PodFetchFunc fetches pods from a k8s Client.
type PodFetchFunc func() ([]kubeApiCore.Pod, error)

// NewPodFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
func NewPodFetch(a istioKube.ExtendedClient, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		pods, err := a.PodsForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			return nil, err
		}
		return pods.Items, nil
	}
}

// NewSinglePodFetch creates a new PodFetchFunction that fetches a single pod matching the given label selectors.
func NewSinglePodFetch(a istioKube.ExtendedClient, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		pods, err := a.PodsForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			return nil, err
		}

		if len(pods.Items) == 0 {
			return nil, fmt.Errorf("no matching pod found for selectors: %v", selectors)
		}

		if len(pods.Items) > 1 {
			scopes.Framework.Warnf("More than one pod found matching selectors: %v", selectors)
		}

		return []kubeApiCore.Pod{pods.Items[0]}, nil
	}
}

// NewPodMustFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
// If no pods are found, an error is returned
func NewPodMustFetch(a istioKube.ExtendedClient, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		pods, err := a.PodsForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			return nil, err
		}
		if len(pods.Items) == 0 {
			return nil, fmt.Errorf("no pods found for %v", selectors)
		}
		return pods.Items, nil
	}
}

// CheckPodsAreReady checks whether the pods that are selected by the given function is in ready state or not.
func CheckPodsAreReady(fetchFunc PodFetchFunc) ([]kubeApiCore.Pod, error) {
	scopes.Framework.Infof("Checking pods ready...")

	fetched, err := fetchFunc()
	if err != nil {
		scopes.Framework.Infof("Failed retrieving pods: %v", err)
		return nil, err
	}

	for i, p := range fetched {
		msg := "Ready"
		if e := istioKube.CheckPodReady(&p); e != nil {
			msg = e.Error()
			err = multierror.Append(err, fmt.Errorf("%s/%s: %s", p.Namespace, p.Name, msg))
		}
		scopes.Framework.Infof("  [%2d] %45s %15s (%v)", i, p.Name, p.Status.Phase, msg)
	}

	if err != nil {
		return nil, err
	}

	return fetched, nil
}

// DeleteOptionsForeground creates new delete options that will block until the operation completes.
func DeleteOptionsForeground() kubeApiMeta.DeleteOptions {
	propagationPolicy := kubeApiMeta.DeletePropagationForeground
	gracePeriod := int64(0)
	return kubeApiMeta.DeleteOptions{
		PropagationPolicy:  &propagationPolicy,
		GracePeriodSeconds: &gracePeriod,
	}
}

// WaitUntilPodsAreReady waits until the pod with the name/namespace is in ready state.
func WaitUntilPodsAreReady(fetchFunc PodFetchFunc, opts ...retry.Option) ([]kubeApiCore.Pod, error) {
	var pods []kubeApiCore.Pod
	_, err := retry.Do(func() (interface{}, bool, error) {

		scopes.Framework.Infof("Checking pods ready...")

		fetched, err := CheckPodsAreReady(fetchFunc)
		if err != nil {
			return nil, false, err
		}
		pods = fetched
		return nil, true, nil
	}, newRetryOptions(opts...)...)

	return pods, err
}

// WaitUntilServiceEndpointsAreReady will wait until the service with the given name/namespace is present, and have at least
// one usable endpoint.
func WaitUntilServiceEndpointsAreReady(a kubernetes.Interface, ns string, name string,
	opts ...retry.Option) (*kubeApiCore.Service, *kubeApiCore.Endpoints, error) {
	var service *kubeApiCore.Service
	var endpoints *kubeApiCore.Endpoints
	err := retry.UntilSuccess(func() error {
		s, err := a.CoreV1().Services(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
		if err != nil {
			return err
		}

		eps, err := a.CoreV1().Endpoints(ns).Get(context.TODO(), name, kubeApiMeta.GetOptions{})
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

// WaitForSecretToExist waits for the given secret up to the given waitTime.
func WaitForSecretToExist(a kubernetes.Interface, namespace, name string, waitTime time.Duration) (*kubeApiCore.Secret, error) {
	secret := a.CoreV1().Secrets(namespace)

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
func WaitForSecretToExistOrFail(t test.Failer, a kubernetes.Interface, namespace, name string,
	waitTime time.Duration) *kubeApiCore.Secret {
	t.Helper()
	s, err := WaitForSecretToExist(a, namespace, name, waitTime)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func WaitForNamespaceDeletion(a kubernetes.Interface, ns string, opts ...retry.Option) error {
	_, err := retry.Do(func() (interface{}, bool, error) {
		_, err2 := a.CoreV1().Namespaces().Get(context.TODO(), ns, kubeApiMeta.GetOptions{})
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

// NamespaceExists returns true if the given namespace exists.
func NamespaceExists(a kubernetes.Interface, ns string) bool {
	allNs, err := a.CoreV1().Namespaces().List(context.TODO(), kubeApiMeta.ListOptions{})
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

func newRetryOptions(opts ...retry.Option) []retry.Option {
	out := make([]retry.Option, 0, 2+len(opts))
	out = append(out, defaultRetryTimeout, defaultRetryDelay)
	out = append(out, opts...)
	return out
}
