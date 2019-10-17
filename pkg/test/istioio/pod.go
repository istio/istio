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

var _ Step = SinglePodWait("")
var _ Step = MultiPodWait("")

// PodWait is a Step that waits for pods to be deployed.
type PodWait func(ctx Context) kube.PodFetchFunc

func (s PodWait) run(ctx Context) {
	ctx.Helper()

	if _, err := ctx.Env.WaitUntilPodsAreReady(s(ctx)); err != nil {
		ctx.Fatal("failed waiting for pods to start: %v", err)
	}
}

// SinglePodWait waits for a single pod matching the given label and selectors.
func SinglePodWait(namespace string, selectors ...string) PodWait {
	return func(ctx Context) kube.PodFetchFunc {
		ctx.Helper()
		return ctx.Env.NewSinglePodFetch(namespace, selectors...)
	}
}

/// MultiPodWait waits for multiple pods that match the given labels and selectors.
func MultiPodWait(namespace string, selectors ...string) PodWait {
	return func(ctx Context) kube.PodFetchFunc {
		ctx.Helper()
		return ctx.Env.NewPodFetch(namespace, selectors...)
	}
}
