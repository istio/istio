package xds

import (
	"reflect"
	"testing"
	"time"

	"istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/config/memory"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/test"
)

func TestMemstore(t *testing.T) {
	memory.NewController(memory.Make(collections.All))
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

	tmplB = &v1alpha3.WorkloadGroup{
		Template: &v1alpha3.WorkloadEntry{
			Ports:          map[string]uint32{"http": 80},
			Labels:         map[string]string{"app": "b"},
			Weight:         1,
			ServiceAccount: "sa-a",
		},
	}
	wgB = config.Config{
		Meta: config.Meta{
			GroupVersionKind: gvk.WorkloadGroup,
			Namespace:        "b",
			Name:             "wg-b",
			Labels: map[string]string{
				"grouplabel": "notonentry",
			},
		},
		Spec:   tmplB,
		Status: nil,
	}
)

func TestAutoregisterWorkloadEntryFailure(t *testing.T) {
	store := memory.NewController(memory.Make(collections.All))
	ig := NewInternalGen(&DiscoveryServer{instanceID: "pilot-1"})
	ig.Store = store
	createOrFail(t, store, wgA)

	t.Run("proxy missing group", func(t *testing.T) {

	})

}

func TestAutoregisterWorkloadEntry(t *testing.T) {
	features.WorkloadEntryCleanupGracePeriod = 5 * time.Second
	store := memory.NewController(memory.Make(collections.All))
	ig1 := NewInternalGen(&DiscoveryServer{instanceID: "pilot-1"})
	ig1.Store = store
	ig2 := NewInternalGen(&DiscoveryServer{instanceID: "pilot-2"})
	ig2.Store = store

	createOrFail(t, store, wgA)
	createOrFail(t, store, wgB)

	t.Run("autoregister", func(t *testing.T) {
		ig1.RegisterWorkload(fakeProxy("1.2.3.4", "wg-a", "a", ""))
		cfg := store.Get(gvk.WorkloadEntry, "wg-a-1.2.3.4", "a")
		if cfg == nil {
			t.Fatal("expected WorkloadEntry to be created.")
		}
		we := cfg.Spec.(*v1alpha3.WorkloadEntry)
		if !reflect.DeepEqual(cfg.Labels, map[string]string{"app": "a", "merge": "me"}) {
			t.Fatal("expected metadata labels to be merged with WorkloadGroup labels")
		}
		if !reflect.DeepEqual(we.Ports, tmplA.Template.Ports) {
			t.Fatal("expected ports from WorkloadGroup")
		}
		if we.Address != "1.2.3.4" {
			t.Fatal("expected to maintain address from proxy")
		}
	})
}

func fakeProxy(ip, wg, ns, nw string) (*model.Proxy, *Connection) {
	p := &model.Proxy{
		IPAddresses: []string{ip},
		Metadata: &model.NodeMetadata{
			AutoRegisterGroup: wg,
			Namespace:         ns,
			Network:           nw,
			Labels:            map[string]string{"merge": "me"},
		},
	}
	c := &Connection{Connect: time.Now(), proxy: p}
	return p, c
}

func createOrFail(t test.Failer, store model.ConfigStoreCache, cfg config.Config) {
	if _, err := store.Create(cfg); err != nil {
		t.Fatalf("failed creating %s/%s: %v", cfg.Namespace, cfg.Name, err)
	}
}
