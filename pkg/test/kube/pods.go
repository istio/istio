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
	"fmt"

	kubeApiCore "k8s.io/api/core/v1"

	"istio.io/istio/pkg/test/scopes"
)

// PodFetchFunc fetches pods from the Accessor.
type PodFetchFunc func() ([]kubeApiCore.Pod, error)

// NewPodFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
func NewPodFetch(a Accessor, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		return a.GetPods(namespace, selectors...)
	}
}

// NewSinglePodFetch creates a new PodFetchFunction that fetches a single pod matching the given label selectors.
func NewSinglePodFetch(a Accessor, namespace string, selectors ...string) PodFetchFunc {
	return func() ([]kubeApiCore.Pod, error) {
		list, err := a.GetPods(namespace, selectors...)
		if err != nil {
			return nil, err
		}

		if len(list) == 0 {
			return nil, fmt.Errorf("no matching pod found for selectors: %v", selectors)
		}

		if len(list) > 1 {
			scopes.Framework.Warnf("More than one pod found matching selectors: %v", selectors)
		}

		return []kubeApiCore.Pod{list[0]}, nil
	}
}

// NewPodMustFetch creates a new PodFetchFunction that fetches all pods matching the namespace and label selectors.
// If no pods are found, an error is returned
func NewPodMustFetch(a Accessor, namespace string, selectors ...string) PodFetchFunc {
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
