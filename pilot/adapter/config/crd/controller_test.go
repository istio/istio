// Copyright 2017 Istio Authors
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

package crd

import (
	"testing"
	"time"

	"istio.io/pilot/platform/kube"
	"istio.io/pilot/test/mock"
)

const (
	resync = 1 * time.Second
)

func TestControllerEvents(t *testing.T) {
	cl, ns, cleanup := makeTempClient(t)
	defer cleanup()
	ctl := NewController(cl, kube.ControllerOptions{Namespace: ns, AppNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheEvents(cl, ctl, ns, 5, t)
}

func TestControllerCacheFreshness(t *testing.T) {
	cl, ns, cleanup := makeTempClient(t)
	defer cleanup()
	ctl := NewController(cl, kube.ControllerOptions{Namespace: ns, AppNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheFreshness(ctl, ns, t)
}

func TestControllerClientSync(t *testing.T) {
	cl, ns, cleanup := makeTempClient(t)
	defer cleanup()
	ctl := NewController(cl, kube.ControllerOptions{Namespace: ns, AppNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheSync(cl, ctl, ns, 5, t)
}
