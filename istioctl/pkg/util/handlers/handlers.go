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
	"errors"
	"sort"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/polymorphichelpers"
	"k8s.io/kubectl/pkg/scheme"
	"k8s.io/kubectl/pkg/util/podutils"
)

// InferPodInfo Uses name to infer namespace if the passed name contains namespace information.
// Otherwise uses the namespace value passed into the function
func InferPodInfo(name, defaultNS string) (string, string) {
	return inferNsInfo(name, defaultNS)
}

// inferNsInfo Uses name to infer namespace if the passed name contains namespace information.
// Otherwise uses the namespace value passed into the function
func inferNsInfo(name, namespace string) (string, string) {
	separator := strings.LastIndex(name, ".")
	if separator < 0 {
		return name, namespace
	}

	return name[0:separator], name[separator+1:]
}

//HandleNamespace returns the defaultNamespace if the namespace is empty
func HandleNamespace(ns, defaultNamespace string) string {
	if ns == v1.NamespaceAll {
		ns = defaultNamespace
	}
	return ns
}

// InferPodInfoFromTypedResource gets a pod name, from an expression like Deployment/httpbin, or Deployment/productpage-v1.bookinfo
func InferPodInfoFromTypedResource(name, defaultNS string, factory cmdutil.Factory) (string, string, error) {
	resname, ns := inferNsInfo(name, defaultNS)

	builder := factory.NewBuilder().
		WithScheme(scheme.Scheme, scheme.Scheme.PrioritizedVersionsAllGroups()...).
		NamespaceParam(ns).DefaultNamespace().
		SingleResourceType()
	builder.ResourceNames("pods", resname)
	infos, err := builder.Do().Infos()
	if err != nil {
		return "", "", err
	}
	if len(infos) != 1 {
		return "", "", errors.New("expected a resource")
	}
	namespace, selector, err := polymorphichelpers.SelectorsForObject(infos[0].Object)
	if err != nil {
		return "", "", err
	}
	clientConfig, err := factory.ToRESTConfig()
	if err != nil {
		return "", "", err
	}
	clientset, err := corev1client.NewForConfig(clientConfig)
	if err != nil {
		return "", "", err
	}
	var timeout time.Duration
	// We need to pass in a sorter, and the one used by `kubectl logs` is good enough.
	sortBy := func(pods []*v1.Pod) sort.Interface { return podutils.ByLogging(pods) }
	pod, _, err := polymorphichelpers.GetFirstPod(clientset, namespace, selector.String(), timeout, sortBy)
	if err != nil {
		return "", "", err
	}
	return pod.Name, ns, nil
}
