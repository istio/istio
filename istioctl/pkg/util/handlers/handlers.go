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

package handlers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/util/podutils"
	gatewayapi "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapibeta "sigs.k8s.io/gateway-api/apis/v1beta1"

	"istio.io/api/label"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	kubelib "istio.io/istio/pkg/kube"
)

// InferPodInfo Uses name to infer namespace if the passed name contains namespace information.
// Otherwise uses the namespace value passed into the function
func InferPodInfo(name, defaultNS string) (string, string) {
	return inferNsInfo(name, defaultNS)
}

// inferNsInfo Uses name to infer namespace if the passed name contains namespace information.
// Otherwise uses the namespace value passed into the function
func inferNsInfo(name, namespace string) (string, string) {
	if idx := strings.LastIndex(name, "/"); idx > 0 {
		// If there is a / in it, we need to handle differently. This is resourcetype/name.namespace.
		// However, resourcetype can have . in it as well, so we should only look for namespace after the /.
		separator := strings.LastIndex(name[idx:], ".")
		if separator < 0 {
			return name, namespace
		}

		return name[0 : idx+separator], name[idx+separator+1:]
	}
	separator := strings.LastIndex(name, ".")
	if separator < 0 {
		return name, namespace
	}

	return name[0:separator], name[separator+1:]
}

func InferPodsFromTypedResource(name, defaultNS string, factory cmdutil.Factory) ([]string, string, error) {
	resname, ns := inferNsInfo(name, defaultNS)
	if !strings.Contains(resname, "/") {
		return []string{resname}, ns, nil
	}
	client, podName, namespace, selector, err := getClientForResource(resname, ns, factory)
	if err != nil {
		return []string{}, "", err
	}
	if podName != "" {
		return []string{podName}, namespace, err
	}

	options := metav1.ListOptions{LabelSelector: selector}
	podList, err := client.Pods(namespace).List(context.TODO(), options)
	if err != nil {
		return []string{}, "", err
	}
	pods := []string{}
	for i := range podList.Items {
		pods = append(pods, podList.Items[i].Name)
	}

	return pods, namespace, nil
}

func getClientForResource(resname, ns string, factory cmdutil.Factory) (*corev1client.CoreV1Client, string, string, string, error) {
	// Pod is referred to using something like "deployment/httpbin".  Use the kubectl
	// libraries to look up the resource name, find the pods it selects, and return
	// one of those pods.
	builder := factory.NewBuilder().
		WithScheme(kubelib.IstioScheme, kubelib.IstioScheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(ns).DefaultNamespace().
		SingleResourceType()
	builder.ResourceNames("pods", resname)
	infos, err := builder.Do().Infos()
	if err != nil {
		return nil, "", "", "", fmt.Errorf("failed retrieving: %v in the %q namespace", err, ns)
	}
	if len(infos) != 1 {
		return nil, "", "", "", errors.New("expected a resource")
	}
	_, ok := infos[0].Object.(*corev1.Pod)
	if ok {
		// If we got a pod, just use its name
		return nil, infos[0].Name, infos[0].Namespace, "", nil
	}
	namespace, selector, err := SelectorsForObject(infos[0].Object)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("%q does not refer to a pod: %v", resname, err)
	}
	clientConfig, err := factory.ToRESTConfig()
	if err != nil {
		return nil, "", "", "", err
	}
	clientset, err := corev1client.NewForConfig(clientConfig)
	if err != nil {
		return nil, "", "", "", err
	}
	return clientset, "", namespace, selector.String(), nil
}

// this is used for testing. it should not be changed in regular code.
var getFirstPodFunc func(client corev1client.PodsGetter, namespace string, selector string, timeout time.Duration,
	sortBy func([]*corev1.Pod) sort.Interface) (*corev1.Pod, int, error)

// InferPodInfoFromTypedResource gets a pod name, from an expression like Deployment/httpbin, or Deployment/productpage-v1.bookinfo
func InferPodInfoFromTypedResource(name, defaultNS string, factory cmdutil.Factory) (string, string, error) {
	resname, ns := inferNsInfo(name, defaultNS)
	if !strings.Contains(resname, "/") {
		return resname, ns, nil
	}
	client, podName, namespace, selector, err := getClientForResource(resname, ns, factory)
	if err != nil {
		return "", "", err
	}
	if podName != "" {
		return podName, namespace, nil
	}
	// We need to pass in a sorter, and the one used by `kubectl logs` is good enough.
	sortBy := func(pods []*corev1.Pod) sort.Interface { return podutils.ByLogging(pods) }
	timeout := 2 * time.Second
	if getFirstPodFunc == nil {
		getFirstPodFunc = polymorphichelpers.GetFirstPod
	}
	pod, _, err := getFirstPodFunc(client, namespace, selector, timeout, sortBy)
	if err != nil {
		return "", "", fmt.Errorf("no pods match %q", resname)
	}
	return pod.Name, namespace, nil
}

// SelectorsForObject is a fork of upstream function to add additional Istio type support
func SelectorsForObject(object runtime.Object) (namespace string, selector labels.Selector, err error) {
	switch t := object.(type) {
	case *gatewayapi.Gateway:
		if !gateway.IsManaged(&t.Spec) {
			return "", nil, fmt.Errorf("gateway is not a managed gateway")
		}
		namespace = t.Namespace
		selector, err = labels.Parse(label.IoK8sNetworkingGatewayGatewayName.Name + "=" + t.Name)
	case *gatewayapibeta.Gateway:
		if !gateway.IsManaged(&t.Spec) {
			return "", nil, fmt.Errorf("gateway is not a managed gateway")
		}
		namespace = t.Namespace
		selector, err = labels.Parse(label.IoK8sNetworkingGatewayGatewayName.Name + "=" + t.Name)
	default:
		return polymorphichelpers.SelectorsForObject(object)
	}
	return namespace, selector, err
}
