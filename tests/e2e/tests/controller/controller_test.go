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

package controller

import (
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	crd2 "istio.io/istio/pilot/pkg/config/kube/crd"
	crd "istio.io/istio/pilot/pkg/config/kube/crd/controller"
	"istio.io/istio/pilot/pkg/model"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	"istio.io/istio/pilot/test/mock"
	"istio.io/istio/pilot/test/util"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/schema/resource"
)

// Package controller tests the pilot controller using a k8s cluster or standalone apiserver.
// It needs to be separate from pilot tests - it may interfere with the pilot tests by creating
// test resources that may confuse other istio tests or it may be confused by other tests.

// TODO: make changes to k8s ( endpoints in particular ) and verify the proper generation of events.
// This test relies on mocks.

const (
	resync = 1 * time.Second
)

func makeClient(desc collection.Schemas) (*crd.Client, error) {
	cl, err := crd.NewClient("", "", desc, "", &model.DisabledLedger{}, "")
	if err != nil {
		return nil, err
	}

	err = cl.RegisterMockResourceCRD()
	if err != nil {
		return nil, err
	}

	return cl, nil
}

// resolveConfig checks whether to use the in-cluster or out-of-cluster config
func resolveConfig(kubeconfig string) (string, error) {
	// Consistency with kubectl
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
	}
	if kubeconfig == "" {
		home := os.Getenv("HOME")
		defaultCfg := home + "/.kube/config"
		_, err := os.Stat(kubeconfig)
		if err != nil {
			kubeconfig = defaultCfg
		}
	}
	if kubeconfig != "" {
		info, err := os.Stat(kubeconfig)
		if err != nil {
			if os.IsNotExist(err) {
				err = fmt.Errorf("kubernetes configuration file %q does not exist", kubeconfig)
			} else {
				err = multierror.Append(err, fmt.Errorf("kubernetes configuration file %q", kubeconfig))
			}
			return "", err
		}

		// if it's an empty file, switch to in-cluster config
		if info.Size() == 0 {
			log.Println("using in-cluster configuration")
			return "", nil
		}
	}
	return kubeconfig, nil
}

// makeTempClient allocates a namespace and cleans it up on test completion
func makeTempClient(t *testing.T) (*crd.Client, string, func()) {
	kubeconfig, err := resolveConfig("")
	if err != nil {
		t.Fatal(err)
	}
	client, err := kube.CreateInterface(kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	ns, err := util.CreateNamespace(client)
	if err != nil {
		t.Fatal(err.Error())
	}
	cl, err := makeClient(crd2.SupportedSchemas)
	if err != nil {
		t.Fatalf(err.Error())
	}

	// the rest of the test can run in parallel
	t.Parallel()
	return cl, ns, func() { util.DeleteNamespace(client, ns) }
}

func TestTempWorkspace(t *testing.T) {
	client, ns, cleanup := makeTempClient(t)
	defer cleanup()

	t.Run("StoreInvariant", func(t *testing.T) {
		storeInvariant(t, client, ns)
	})
	t.Run("istioConfig", func(t *testing.T) {
		istioConfig(t, client, ns)
	})
	t.Run("controllerEvents", func(t *testing.T) {
		controllerEvents(t, client, ns)
	})
	t.Run("controllerClientSync", func(t *testing.T) {
		controllerClientSync(t, client, ns)
	})
	t.Run("controllerCacheFreshness", func(t *testing.T) {
		controllerCacheFreshness(t, client, ns)
	})
}

func storeInvariant(t *testing.T, client model.ConfigStore, ns string) {
	mock.CheckMapInvariant(client, t, ns, 5)
	log.Println("Check Map Invariant done")
}

func istioConfig(t *testing.T, client model.ConfigStore, ns string) {
	mock.CheckIstioConfigTypes(client, ns, t)
}

func TestUnknownConfig(t *testing.T) {
	desc := collection.SchemasFor(collection.Builder{
		Name: "unknown",
		Resource: resource.Builder{
			Kind:    "UnknownConfig",
			Plural:  "UnknownConfigs",
			Group:   "test",
			Version: "v1",
			Proto:   "test.MockConfig",
		}.MustBuild(),
	}.MustBuild())
	_, err := makeClient(desc)
	if err == nil {
		t.Fatalf("expect client to fail with unknown types")
	}
}

func controllerEvents(t *testing.T, cl *crd.Client, ns string) {
	ctl := crd.NewController(cl, kube.Options{WatchedNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheEvents(cl, ctl, ns, 5, t)
}

func controllerCacheFreshness(t *testing.T, cl *crd.Client, ns string) {
	ctl := crd.NewController(cl, kube.Options{WatchedNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheFreshness(ctl, ns, t)
}

func controllerClientSync(t *testing.T, cl *crd.Client, ns string) {
	ctl := crd.NewController(cl, kube.Options{WatchedNamespace: ns, ResyncPeriod: resync})
	mock.CheckCacheSync(cl, ctl, ns, 5, t)
}
