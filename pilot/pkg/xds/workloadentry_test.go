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

package xds

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/hashicorp/go-multierror"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

func init() {
	features.WorkloadEntryAutoRegistration = true
}

var (
	tmplA = &v1alpha3.WorkloadGroup{
		Template: &v1alpha3.WorkloadEntry{
			Ports:          map[string]uint32{"http": 80},
			Labels:         map[string]string{"app": "a"},
			Weight:         1,
			ServiceAccount: "sa-a",
		},
	}
	wgA = config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadGroup,
			Namespace:        "a",
			Name:             "wg-a",
			Labels: map[string]string{
				"grouplabel": "notonentry",
			},
		},
		Spec:   tmplA,
		Status: nil,
	}
)

func TestNonAutoregisteredWorkloads(t *testing.T) {
	store := memory.NewController(memory.Make(collections.All))
	ig := NewInternalGen(&DiscoveryServer{instanceID: "pilot-1"})
	ig.Store = store
	createOrFail(t, store, wgA)

	cases := map[string]*model.Proxy{
		"missing group":      {IPAddresses: []string{"1.2.3.4"}, Metadata: &model.NodeMetadata{Namespace: wgA.Namespace}},
		"missing ip":         {Metadata: &model.NodeMetadata{Namespace: wgA.Namespace, AutoRegisterGroup: wgA.Name}},
		"missing namespace":  {IPAddresses: []string{"1.2.3.4"}, Metadata: &model.NodeMetadata{AutoRegisterGroup: wgA.Name}},
		"non-existent group": {IPAddresses: []string{"1.2.3.4"}, Metadata: &model.NodeMetadata{Namespace: wgA.Namespace, AutoRegisterGroup: "dne"}},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			ig.RegisterWorkload(tc, &Connection{proxy: tc, Connect: time.Now()})
			items, err := store.List(gvk.WorkloadEntry, model.NamespaceAll)
			if err != nil {
				t.Fatalf("failed listing WorkloadEntry: %v", err)
			}
			if len(items) != 0 {
				t.Fatalf("expected 0 WorkloadEntry")
			}
		})
	}

}

func TestAutoregistrationLifecycle(t *testing.T) {
	if features.WorkloadEntryCleanupGracePeriod != 200*time.Millisecond {
		features.WorkloadEntryCleanupGracePeriod = 200 * time.Millisecond
	}
	ig1, ig2, store := setup(t)
	stopped1 := false
	stop1, stop2 := make(chan struct{}), make(chan struct{})
	defer func() {
		// stop1 should be killed early, as part of test
		if !stopped1 {
			close(stop1)
		}
	}()
	defer close(stop2)
	go ig1.Run(stop1)
	go ig2.Run(stop2)

	p := fakeProxy("1.2.3.4", wgA, "nw1")
	p2 := fakeProxy("1.2.3.4", wgA, "nw2")

	t.Run("initial registration", func(t *testing.T) {
		// simply make sure the entry exists after connecting
		ig1.RegisterWorkload(p, &Connection{proxy: p, Connect: time.Now()})
		checkEntryOrFail(t, store, wgA, p, ig1.Server.instanceID)
	})
	t.Run("multinetwork same ip", func(t *testing.T) {
		// make sure we don't overrwrite a similar entry for a different network
		ig2.RegisterWorkload(p2, &Connection{proxy: p2, Connect: time.Now()})
		checkEntryOrFail(t, store, wgA, p, ig1.Server.instanceID)
		checkEntryOrFail(t, store, wgA, p2, ig2.Server.instanceID)
	})
	t.Run("fast reconnect", func(t *testing.T) {
		t.Run("same instance", func(t *testing.T) {
			// disconnect, make sure entry is still there with disconnect meta
			ig1.QueueUnregisterWorkload(p)
			time.Sleep(features.WorkloadEntryCleanupGracePeriod / 2)
			checkEntryOrFail(t, store, wgA, p, "")
			// reconnect, ensure entry is there with the same instance id
			ig1.RegisterWorkload(p, &Connection{proxy: p, Connect: time.Now()})
			checkEntryOrFail(t, store, wgA, p, ig1.Server.instanceID)
		})
		t.Run("different instance", func(t *testing.T) {
			// disconnect, make sure entry is still there with disconnect metadata
			ig1.QueueUnregisterWorkload(p)
			time.Sleep(features.WorkloadEntryCleanupGracePeriod / 2)
			checkEntryOrFail(t, store, wgA, p, "")
			// reconnect, ensure entry is there with the new instance id
			ig2.RegisterWorkload(p, &Connection{proxy: p, Connect: time.Now()})
			checkEntryOrFail(t, store, wgA, p, ig2.Server.instanceID)
		})
	})
	t.Run("slow reconnect", func(t *testing.T) {
		// disconnect, wait and make sure entry is gone
		ig2.QueueUnregisterWorkload(p)
		retry.UntilSuccessOrFail(t, func() error {
			return checkNoEntry(store, wgA, p)
		})
		// reconnect
		ig1.RegisterWorkload(p, &Connection{proxy: p, Connect: time.Now()})
		checkEntryOrFail(t, store, wgA, p, ig1.Server.instanceID)
	})
	t.Run("garbage collected if pilot stops after disconnect", func(t *testing.T) {
		// disconnect, kill the cleanup queue from the first controller
		ig1.QueueUnregisterWorkload(p)
		// stop processing the delayed close queue in ig1, forces using periodic cleanup
		close(stop1)
		stopped1 = true
		// unfortunately, this retry at worst could be twice as long as the sweep interval
		retry.UntilSuccessOrFail(t, func() error {
			return checkNoEntry(store, wgA, p)
		}, retry.Timeout(time.Until(time.Now().Add(21*features.WorkloadEntryCleanupGracePeriod))))
	})
	// TODO test garbage collection if pilot stops before disconnect meta is set (relies on heartbeat)
}

func setup(t *testing.T) (*InternalGen, *InternalGen, model.ConfigStoreCache) {
	store := memory.NewController(memory.Make(collections.All))
	ig1 := NewInternalGen(&DiscoveryServer{instanceID: "pilot-1"})
	ig1.Store = store
	ig2 := NewInternalGen(&DiscoveryServer{instanceID: "pilot-2"})
	ig2.Store = store
	createOrFail(t, store, wgA)
	return ig1, ig2, store
}

func checkNoEntry(store model.ConfigStoreCache, wg config.Config, proxy *model.Proxy) error {
	name := wg.Name + "-" + proxy.IPAddresses[0]
	if proxy.Metadata.Network != "" {
		name += "-" + proxy.Metadata.Network
	}

	cfg := store.Get(gvk.WorkloadEntry, name, wg.Namespace)
	if cfg != nil {
		return fmt.Errorf("did not expect WorkloadEntry %s/%s to exist", wg.Namespace, name)
	}
	return nil
}

func checkEntry(
	store model.ConfigStoreCache,
	wg config.Config,
	proxy *model.Proxy,
	connectedTo string,
) (err error) {
	name := wg.Name + "-" + proxy.IPAddresses[0]
	if proxy.Metadata.Network != "" {
		name += "-" + proxy.Metadata.Network
	}

	cfg := store.Get(gvk.WorkloadEntry, name, wg.Namespace)
	if cfg == nil {
		err = multierror.Append(fmt.Errorf("expected WorkloadEntry %s/%s to exist", wg.Namespace, name))
		return
	}
	tmpl := wg.Spec.(*v1alpha3.WorkloadGroup)
	we := cfg.Spec.(*v1alpha3.WorkloadEntry)

	// check workload entry specific fields
	if !reflect.DeepEqual(we.Ports, tmpl.Template.Ports) {
		err = multierror.Append(err, fmt.Errorf("expected ports from WorkloadGroup"))
	}
	if we.Address != proxy.IPAddresses[0] {
		err = multierror.Append(fmt.Errorf("entry has address %s; expected %s", we.Address, proxy.IPAddresses[0]))
	}

	// check controller annotations
	if connectedTo != "" {
		if v := cfg.Annotations[WorkloadControllerAnnotation]; v != connectedTo {
			err = multierror.Append(err, fmt.Errorf("expected WorkloadEntry to be updated by %s; got %s", connectedTo, v))
		}
		if _, ok := cfg.Annotations[ConnectedAtAnnotation]; !ok {
			err = multierror.Append(err, fmt.Errorf("expected connection timestamp to be set"))
		}
	} else {
		if _, ok := cfg.Annotations[WorkloadControllerAnnotation]; ok {
			err = multierror.Append(err, fmt.Errorf("expected WorkloadEntry have controller annotation unset"))
		}
		if _, ok := cfg.Annotations[DisconnectedAtAnnotation]; !ok {
			err = multierror.Append(err, fmt.Errorf("expected disconnection timestamp to be set"))
		}
	}

	// check all labels are copied to the WorkloadEntry
	if !reflect.DeepEqual(cfg.Labels, we.Labels) {
		err = multierror.Append(err, fmt.Errorf("spec labels on WorkloadEntry should match meta labels"))
	}
	for k, v := range tmpl.Template.Labels {
		if _, ok := proxy.Metadata.Labels[k]; ok {
			// would be overwritten
			continue
		}
		if we.Labels[k] != v {
			err = multierror.Append(err, fmt.Errorf("labels missing on WorkloadEntry: %s=%s from template", k, v))
		}
	}
	for k, v := range proxy.Metadata.Labels {
		if we.Labels[k] != v {
			err = multierror.Append(err, fmt.Errorf("labels missing on WorkloadEntry: %s=%s from proxy meta", k, v))
		}
	}
	return
}

func checkEntryOrFail(
	t test.Failer,
	store model.ConfigStoreCache,
	wg config.Config,
	proxy *model.Proxy,
	connectedTo string,
) {
	if err := checkEntry(store, wg, proxy, connectedTo); err != nil {
		t.Fatal(err)
	}
}

func fakeProxy(ip string, wg config.Config, nw string) *model.Proxy {
	return &model.Proxy{
		IPAddresses: []string{ip},
		Metadata: &model.NodeMetadata{
			AutoRegisterGroup: wg.Name,
			Namespace:         wg.Namespace,
			Network:           nw,
			Labels:            map[string]string{"merge": "me"},
		},
	}
}

// createOrFail wraps config creation with convience for failing tests
func createOrFail(t test.Failer, store model.ConfigStoreCache, cfg config.Config) {
	if _, err := store.Create(cfg); err != nil {
		t.Fatalf("failed creating %s/%s: %v", cfg.Namespace, cfg.Name, err)
	}
}
