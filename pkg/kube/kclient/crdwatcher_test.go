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

package kclient_test

import (
	"testing"

	"go.uber.org/atomic"

	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

func TestCRDWatcher(t *testing.T) {
	stop := test.NewStop(t)
	c := kube.NewFakeClient()

	clienttest.MakeCRD(t, c, gvr.WasmPlugin)
	wasmCalls := atomic.NewInt32(0)

	clienttest.MakeCRD(t, c, gvr.VirtualService)
	vsCalls := atomic.NewInt32(0)

	clienttest.MakeCRD(t, c, gvr.GatewayClass)

	ctl := kube.NewCrdWatcher(c)
	// Created before informer runs
	assert.Equal(t, ctl.KnownOrCallback(gvr.VirtualService, func(s <-chan struct{}) {
		assert.Equal(t, s, stop)
		vsCalls.Inc()
	}), false)

	c.RunAndWait(stop)
	// Created before run
	assert.Equal(t, ctl.KnownOrCallback(gvr.WasmPlugin, func(s <-chan struct{}) {
		assert.Equal(t, s, stop)
		wasmCalls.Inc()
	}), false)
	assert.Equal(t, vsCalls.Load(), 0)
	assert.Equal(t, wasmCalls.Load(), 0)
	ctl.Run(stop)
	kube.WaitForCacheSync("test", stop, ctl.HasSynced)

	assert.EventuallyEqual(t, vsCalls.Load, 1)
	assert.EventuallyEqual(t, wasmCalls.Load, 1)

	// created once running
	assert.Equal(t, ctl.KnownOrCallback(gvr.GatewayClass, func(s <-chan struct{}) {
		t.Fatalf("callback should not be called")
	}), true)

	// Create CRD later
	saCalls := atomic.NewInt32(0)
	// When should return false
	assert.Equal(t, ctl.KnownOrCallback(gvr.ServiceAccount, func(s <-chan struct{}) {
		assert.Equal(t, s, stop)
		saCalls.Inc()
	}), false)
	clienttest.MakeCRD(t, c, gvr.ServiceAccount)
	// And call the callback when the CRD is created
	assert.EventuallyEqual(t, saCalls.Load, 1)
}
