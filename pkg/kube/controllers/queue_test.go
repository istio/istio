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

package controllers

import (
	"testing"

	"go.uber.org/atomic"
	"k8s.io/apimachinery/pkg/types"

	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func TestQueue(t *testing.T) {
	handles := atomic.NewInt32(0)
	q := NewQueue("custom", WithReconciler(func(key types.NamespacedName) error {
		handles.Inc()
		return nil
	}))
	q.Add(types.NamespacedName{Name: "something"})
	go q.Run(test.NewStop(t))
	retry.UntilOrFail(t, q.HasSynced)
	if got := handles.Load(); got != 1 {
		t.Fatalf("expected 1 handle, got %v", got)
	}
}
