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
	"net"
	"time"

	"github.com/hashicorp/go-multierror"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	istioKube "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/scopes"
	"istio.io/istio/pkg/test/util/retry"
)

var (
	defaultRetryTimeout = retry.Timeout(time.Minute * 10)
	defaultRetryDelay   = retry.BackoffDelay(time.Millisecond * 200)

	ErrNoPodsFetched = fmt.Errorf("no pods fetched")
)

type (
	// PodFetchFunc fetches pods from a k8s Client.
	PodFetchFunc func() ([]corev1.Pod, error)

	// SvcFetchFunc fetches services from a k8s Client.
	SvcFetchFunc func() ([]corev1.Service, error)
)

// NewPodFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
func NewPodFetch(a istioKube.CLIClient, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]corev1.Pod, error) {
		pods, err := a.PodsForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			return nil, err
		}
		return pods.Items, nil
	}
}

// NewSinglePodFetch creates a new PodFetchFunction that fetches a single pod matching the given label selectors.
func NewSinglePodFetch(a istioKube.CLIClient, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]corev1.Pod, error) {
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

		return []corev1.Pod{pods.Items[0]}, nil
	}
}

// NewPodMustFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
// If no pods are found, an error is returned
func NewPodMustFetch(a istioKube.CLIClient, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]corev1.Pod, error) {
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
func CheckPodsAreReady(fetchFunc PodFetchFunc) ([]corev1.Pod, error) {
	scopes.Framework.Infof("Checking pods ready...")

	fetched, err := fetchFunc()
	if err != nil {
		scopes.Framework.Infof("Failed retrieving pods: %v", err)
		return nil, err
	}

	if len(fetched) == 0 {
		scopes.Framework.Infof("No pods found...")
		return nil, ErrNoPodsFetched
	}

	for i, p := range fetched {
		msg := "Ready"
		if e := istioKube.CheckPodReadyOrComplete(&p); e != nil {
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

// NewServiceFetch creates a new ServiceFetchFunction that fetches all services matching the namespace and label selectors.
func NewServiceFetch(a istioKube.CLIClient, namespace string, selectors ...string) SvcFetchFunc {
	return func() ([]corev1.Service, error) {
		services, err := a.ServicesForSelector(context.TODO(), namespace, selectors...)
		if err != nil {
			return nil, err
		}
		return services.Items, nil
	}
}

// DeleteOptionsForeground creates new delete options that will block until the operation completes.
func DeleteOptionsForeground() metav1.DeleteOptions {
	propagationPolicy := metav1.DeletePropagationForeground
	gracePeriod := int64(0)
	return metav1.DeleteOptions{
		PropagationPolicy:  &propagationPolicy,
		GracePeriodSeconds: &gracePeriod,
	}
}

// WaitUntilPodsAreReady waits until the pod with the name/namespace is in ready state.
func WaitUntilPodsAreReady(fetchFunc PodFetchFunc, opts ...retry.Option) ([]corev1.Pod, error) {
	var pods []corev1.Pod
	err := retry.UntilSuccess(func() error {
		scopes.Framework.Infof("Checking pods ready...")

		fetched, err := CheckPodsAreReady(fetchFunc)
		if err != nil {
			return err
		}
		pods = fetched
		return nil
	}, newRetryOptions(opts...)...)

	return pods, err
}

// WaitUntilServiceEndpointsAreReady will wait until the service with the given name/namespace is present, and have at least
// one usable endpoint.
// Endpoints is deprecated in k8s >=1.33, but we should still support it.
// nolint: staticcheck
func WaitUntilServiceEndpointsAreReady(a kubernetes.Interface, ns string, name string,
	opts ...retry.Option,
) (*corev1.Service, *corev1.Endpoints, error) {
	var service *corev1.Service
	var endpoints *corev1.Endpoints
	err := retry.UntilSuccess(func() error {
		s, err := a.CoreV1().Services(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}

		eps, err := a.CoreV1().Endpoints(ns).Get(context.TODO(), name, metav1.GetOptions{})
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

func WaitUntilServiceLoadBalancerReady(a kubernetes.Interface, ns string, name string, opts ...retry.Option) (string, error) {
	var addr string
	err := retry.UntilSuccess(func() error {
		s, err := a.CoreV1().Services(ns).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if len(s.Status.LoadBalancer.Ingress) == 0 {
			return fmt.Errorf("no LB assigned")
		}
		lb := s.Status.LoadBalancer.Ingress[0]
		if lb.IP != "" {
			addr = lb.IP
			return nil
		}
		if lb.Hostname != "" {
			addr = lb.Hostname
			return nil
		}
		return fmt.Errorf("unexpected LoadBalancer %v", lb)
	}, newRetryOptions(opts...)...)
	return addr, err
}

// WaitForSecretToExist waits for the given secret up to the given waitTime.
func WaitForSecretToExist(a kubernetes.Interface, namespace, name string, waitTime time.Duration) (*corev1.Secret, error) {
	secret := a.CoreV1().Secrets(namespace)

	watch, err := secret.Watch(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to set up watch for secret (error: %v)", err)
	}
	events := watch.ResultChan()

	startTime := time.Now()
	for {
		select {
		case event := <-events:
			secret := event.Object.(*corev1.Secret)
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
	waitTime time.Duration,
) *corev1.Secret {
	t.Helper()
	s, err := WaitForSecretToExist(a, namespace, name, waitTime)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// WaitForNamespaceDeletion waits until a namespace is deleted.
func WaitForNamespaceDeletion(a kubernetes.Interface, ns string, opts ...retry.Option) error {
	return retry.UntilSuccess(func() error {
		_, err := a.CoreV1().Namespaces().Get(context.TODO(), ns, metav1.GetOptions{})
		if err == nil {
			return fmt.Errorf("namespace %v still exists", ns)
		}

		if errors.IsNotFound(err) {
			return nil
		}

		return err
	}, newRetryOptions(opts...)...)
}

// NamespaceExists returns true if the given namespace exists.
func NamespaceExists(a kubernetes.Interface, ns string) bool {
	allNs, err := a.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
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

// MutatingWebhookConfigurationsExists returns true if all the given mutating webhook configs exist.
func MutatingWebhookConfigurationsExists(a kubernetes.Interface, names []string) bool {
	cfgs, err := a.AdmissionregistrationV1().MutatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false
	}

	var existing []string
	for _, cfg := range cfgs.Items {
		existing = append(existing, cfg.Name)
	}

	return checkAllNamesExist(names, existing)
}

// ValidatingWebhookConfigurationsExists returns true if all the given validating webhook configs exist.
func ValidatingWebhookConfigurationsExists(a kubernetes.Interface, names []string) bool {
	cfgs, err := a.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return false
	}

	var existing []string
	for _, cfg := range cfgs.Items {
		existing = append(existing, cfg.Name)
	}

	return checkAllNamesExist(names, existing)
}

func checkAllNamesExist(names []string, haystack []string) bool {
	if len(haystack) < len(names) {
		return false
	}

	for _, name := range names {
		if !slices.Contains(haystack, name) {
			return false
		}
	}

	return true
}

// Resolve domain name and return ip address.
// By default, return ipv4 address and if missing, return ipv6.
func resolveHostDomainToIP(hostDomain string) (string, error) {
	ips, err := net.LookupIP(hostDomain)
	if err != nil {
		return "", err
	}

	var ipv6Addr string

	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String(), nil
		} else if ipv6Addr == "" {
			ipv6Addr = ip.String()
		}
	}

	if ipv6Addr != "" {
		return ipv6Addr, nil
	}

	return "", fmt.Errorf("no IP address found for hostname: %s", hostDomain)
}

// When the Ingress is a domain name (in public cloud), it might take a bit of time to make it reachable.
func WaitUntilReachableIngress(hostDomain string) (string, error) {
	var ip string
	err := retry.UntilSuccess(func() error {
		ipAddr, err := resolveHostDomainToIP(hostDomain)
		if err != nil {
			return err
		}
		ip = ipAddr
		return nil
	}, retry.Timeout(90*time.Second), retry.BackoffDelay(1*time.Second))

	return ip, err
}
