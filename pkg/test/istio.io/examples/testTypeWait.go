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
package examples

import (
	"testing"

	"istio.io/istio/pkg/test/framework/components/environment/kube"
	kubePkg "istio.io/istio/pkg/test/kube"
)

//KubePodFetchFunc accepts a Kubernetes environment and returns a PodFetchFunction
type KubePodFetchFunc func(env *kube.Environment) kubePkg.PodFetchFunc

//WaitForPodtestType is a wrapper to wait for pods to deploy.
type waitForPodTestType struct {
	fetchFunc KubePodFetchFunc
}

type PodWaitType string

func newWaitForPodTestType(fetchFunc KubePodFetchFunc) waitForPodTestType {
	return waitForPodTestType{
		fetchFunc: fetchFunc,
	}
}

func (test waitForPodTestType) Run(env *kube.Environment, t *testing.T) (string, error) {
	_, err := env.WaitUntilPodsAreReady(test.fetchFunc(env))
	return "", err
}

func (test waitForPodTestType) Copy(destination string) error {
	return nil
}

func (test waitForPodTestType) String() string {
	return "waiting for pods"
}

// NewSinglePodFetch creates a new PodFetchFunction that fetches a single pod matching the given label and selectors.
func NewSinglePodFetch(namespace string, selectors ...string) KubePodFetchFunc {
	return func(env *kube.Environment) kubePkg.PodFetchFunc {
		return env.NewSinglePodFetch(namespace, selectors...)
	}
}

/// NewPodFetch creates a new PodFetchFunction that fetches all pods matching the given labels and selectors.
func NewMultiPodFetch(namespace string, selectors ...string) KubePodFetchFunc {
	return func(env *kube.Environment) kubePkg.PodFetchFunc {
		return env.NewPodFetch(namespace, selectors...)
	}
}
