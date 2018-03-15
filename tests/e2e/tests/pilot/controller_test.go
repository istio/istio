// Copyright 2018 Istio Authors
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

package pilot

import (
	"os"
	"testing"
	"time"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/serviceregistry/kube"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pilot/test/util"
)

const (
	resync = 1 * time.Second
)

func makeClient(t *testing.T, desc model.ConfigDescriptor) (*crd.Client, error) {
	cl, err := crd.NewClient(os.Getenv("KUBECONFIG"), desc, "")
	if err != nil {
		return nil, err
	}

	err = cl.RegisterResources()
	if err != nil {
		return nil, err
	}

	// TODO(kuat) initial watch always fails, takes time to register, keep
	// around as a work-around
	// kr.DeregisterResources()

	return cl, nil
}

// makeTempClient allocates a namespace and cleans it up on test completion
func makeTempClient(t *testing.T) (*crd.Client, string, func()) {
	_, client, err := kube.CreateInterface(os.Getenv("KUBECONFIG"))
	if err != nil {
		t.Fatal(err)
	}
	ns, err := util.CreateNamespace(client)
	if err != nil {
		t.Fatal(err.Error())
	}
	desc := append(model.IstioConfigTypes, mock.Types...)
	cl, err := makeClient(t, desc)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// the rest of the test can run in parallel
	t.Parallel()
	return cl, ns, func() { util.DeleteNamespace(client, ns) }
}

func TestStoreInvariant(t *testing.T) {
	client, ns, cleanup := makeTempClient(t)
	defer cleanup()
	mock.CheckMapInvariant(client, t, ns, 5)
}

func TestIstioConfig(t *testing.T) {
	client, ns, cleanup := makeTempClient(t)
	defer cleanup()
	mock.CheckIstioConfigTypes(client, ns, t)
}

func TestUnknownConfig(t *testing.T) {
	desc := model.ConfigDescriptor{model.ProtoSchema{
		Type:        "unknown-config",
		Plural:      "unknown-configs",
		Group:       "test",
		Version:     "v1",
		MessageName: "test.MockConfig",
		Validate:    nil,
	}}
	_, err := makeClient(t, desc)
	if err == nil {
		t.Fatalf("expect client to fail with unknown types")
	}
}

func TestControllerEvents(t *testing.T) {
	cl, ns, cleanup := makeTempClient(t)
	defer cleanup()
	ctl := crd.NewController(cl, kube.ControllerOptions{WatchedNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheEvents(cl, ctl, ns, 5, t)
}

func TestControllerCacheFreshness(t *testing.T) {
	cl, ns, cleanup := makeTempClient(t)
	defer cleanup()
	ctl := crd.NewController(cl, kube.ControllerOptions{WatchedNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheFreshness(ctl, ns, t)
}

func TestControllerClientSync(t *testing.T) {
	cl, ns, cleanup := makeTempClient(t)
	defer cleanup()
	ctl := crd.NewController(cl, kube.ControllerOptions{WatchedNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheSync(cl, ctl, ns, 5, t)
}
