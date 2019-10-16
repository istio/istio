// Copyright 2019 Istio Authors
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

package istioio

import (
	"istio.io/istio/pkg/test/kube"
)

//KubePodFetchFunc accepts a Kubernetes environment and returns a PodFetchFunction
type KubePodFetchFunc func(ctx Context) kube.PodFetchFunc

var _ step = &podWaitStep{}

//WaitForPodtestType is a wrapper to wait for pods to deploy.
type podWaitStep struct {
	fn KubePodFetchFunc
}

func newPodWaitStep(fn KubePodFetchFunc) step {
	return &podWaitStep{
		fn: fn,
	}
}

func (s *podWaitStep) Run(ctx Context) {
	if _, err := ctx.Env.WaitUntilPodsAreReady(s.fn(ctx)); err != nil {
		ctx.Fatal("failed pod wait step: %v", err)
	}
}

func (s *podWaitStep) String() string {
	return "waiting for pods"
}

// NewSinglePodFetch creates a new PodFetchFunction that fetches a single pod matching the given label and selectors.
func NewSinglePodFetch(namespace string, selectors ...string) KubePodFetchFunc {
	return func(ctx Context) kube.PodFetchFunc {
		return ctx.Env.NewSinglePodFetch(namespace, selectors...)
	}
}

/// NewPodFetch creates a new PodFetchFunction that fetches all pods matching the given labels and selectors.
func NewMultiPodFetch(namespace string, selectors ...string) KubePodFetchFunc {
	return func(ctx Context) kube.PodFetchFunc {
		return ctx.Env.NewPodFetch(namespace, selectors...)
	}
}
