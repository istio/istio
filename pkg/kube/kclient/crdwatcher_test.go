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
	"sigs.k8s.io/gateway-api/pkg/consts"

	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/kclient/clienttest"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
)

// TestCRDWatcherRace tests for a previous bug where callbacks may be skipped if added during a handler
func TestCRDWatcherRace(t *testing.T) {
	stop := test.NewStop(t)
	c := kube.NewFakeClient()
	ctl := c.CrdWatcher()
	vsCalls := atomic.NewInt32(0)

	// Race callback and CRD creation
	go func() {
		if ctl.KnownOrCallback(gvr.VirtualService, func(s <-chan struct{}) {
			assert.Equal(t, s, stop)
			// Happened async
			vsCalls.Inc()
		}) {
			// Happened sync
			vsCalls.Inc()
		}
	}()
	clienttest.MakeCRD(t, c, gvr.VirtualService)
	c.RunAndWait(stop)
	assert.EventuallyEqual(t, vsCalls.Load, 1)
}

func TestCRDWatcher(t *testing.T) {
	stop := test.NewStop(t)
	c := kube.NewFakeClient()

	clienttest.MakeCRD(t, c, gvr.VirtualService)
	vsCalls := atomic.NewInt32(0)

	clienttest.MakeCRD(t, c, gvr.GatewayClass)

	ctl := c.CrdWatcher()
	// Created before informer runs
	assert.Equal(t, ctl.KnownOrCallback(gvr.VirtualService, func(s <-chan struct{}) {
		assert.Equal(t, s, stop)
		vsCalls.Inc()
	}), false)

	c.RunAndWait(stop)
	assert.EventuallyEqual(t, vsCalls.Load, 1)

	// created once running
	assert.Equal(t, ctl.KnownOrCallback(gvr.GatewayClass, func(s <-chan struct{}) {
		t.Fatal("callback should not be called")
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

func TestCRDWatcherMinimumVersion(t *testing.T) {
	stop := test.NewStop(t)
	c := kube.NewFakeClient()

	clienttest.MakeCRDWithAnnotations(t, c, gvr.GRPCRoute, map[string]string{
		consts.BundleVersionAnnotation: "v1.0.0",
	})
	calls := atomic.NewInt32(0)

	ctl := c.CrdWatcher()
	// Created before informer runs: not ready yet
	assert.Equal(t, ctl.KnownOrCallback(gvr.GRPCRoute, func(s <-chan struct{}) {
		assert.Equal(t, s, stop)
		calls.Inc()
	}), false)

	c.RunAndWait(stop)

	// Still not ready
	assert.Equal(t, calls.Load(), 0)

	// Upgrade it to v1.1, which is allowed
	clienttest.MakeCRDWithAnnotations(t, c, gvr.GRPCRoute, map[string]string{
		consts.BundleVersionAnnotation: "v1.1.0",
	})
	assert.EventuallyEqual(t, calls.Load, 1)
}
