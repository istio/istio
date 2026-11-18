package ambient

import (
	"context"
	"net/netip"
	"testing"

	corev1 "k8s.io/api/core/v1"
	networking "k8s.io/api/networking/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"

	meshapi "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

func TestIPAllocatorReconcileServiceCIDR(t *testing.T) {
	ipallocator := &IPAllocator{
		v4Allocator: allocator{cidrIndex: make(map[netip.Prefix]int)},
		v6Allocator: allocator{cidrIndex: make(map[netip.Prefix]int)},
	}
	initial := &networking.ServiceCIDR{
		Spec: networking.ServiceCIDRSpec{CIDRs: []string{"10.0.0.0/24", "fd00::/64"}},
	}
	updated := &networking.ServiceCIDR{
		Spec: networking.ServiceCIDRSpec{CIDRs: []string{"10.1.0.0/24", "fd01::/64"}},
	}
	for _, tt := range []struct {
		name   string
		events []krt.Event[*networking.ServiceCIDR]
		wantV4 map[netip.Prefix]int
		wantV6 map[netip.Prefix]int
	}{
		{
			name:   "add",
			events: []krt.Event[*networking.ServiceCIDR]{{Event: controllers.EventAdd, New: &initial}},
			wantV4: map[netip.Prefix]int{netip.MustParsePrefix("10.0.0.0/24"): 0},
			wantV6: map[netip.Prefix]int{netip.MustParsePrefix("fd00::/64"): 0},
		},
		{
			name:   "update",
			events: []krt.Event[*networking.ServiceCIDR]{{Event: controllers.EventUpdate, Old: &initial, New: &updated}},
			wantV4: map[netip.Prefix]int{netip.MustParsePrefix("10.1.0.0/24"): 0},
			wantV6: map[netip.Prefix]int{netip.MustParsePrefix("fd01::/64"): 0},
		},
		{
			name:   "delete",
			events: []krt.Event[*networking.ServiceCIDR]{{Event: controllers.EventDelete, Old: &updated}},
			wantV4: map[netip.Prefix]int{},
			wantV6: map[netip.Prefix]int{},
		},
		{
			name: "batch",
			events: []krt.Event[*networking.ServiceCIDR]{
				{Event: controllers.EventAdd, New: &initial},
				{Event: controllers.EventUpdate, Old: &initial, New: &updated},
				{Event: controllers.EventDelete, Old: &updated},
				{Event: controllers.EventAdd, New: &initial},
			},
			wantV4: map[netip.Prefix]int{netip.MustParsePrefix("10.0.0.0/24"): 0},
			wantV6: map[netip.Prefix]int{netip.MustParsePrefix("fd00::/64"): 0},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.NoError(t, ipallocator.reconcile(&tt.events))
			assert.Equal(t, ipallocator.v4Allocator.cidrIndex, tt.wantV4)
			assert.Equal(t, ipallocator.v6Allocator.cidrIndex, tt.wantV6)
		})
	}
}

func TestIPAllocatorReconcileIPAddress(t *testing.T) {
	address := &networking.IPAddress{ObjectMeta: metav1.ObjectMeta{Name: "10.0.0.42"}}
	v6Address := &networking.IPAddress{ObjectMeta: metav1.ObjectMeta{Name: "fd00::42"}}
	invalid := &networking.IPAddress{ObjectMeta: metav1.ObjectMeta{Name: "invalid"}}
	v4 := netip.MustParseAddr(address.Name)
	v6 := netip.MustParseAddr(v6Address.Name)
	for _, tt := range []struct {
		name   string
		events []krt.Event[*networking.IPAddress]
		used   map[netip.Addr]bool
		want   map[netip.Addr]bool
	}{
		{
			name:   "add",
			events: []krt.Event[*networking.IPAddress]{{Event: controllers.EventAdd, New: &address}},
			want:   map[netip.Addr]bool{v4: true},
		},
		{
			name:   "update",
			events: []krt.Event[*networking.IPAddress]{{Event: controllers.EventUpdate, Old: &address, New: &address}},
			want:   map[netip.Addr]bool{v4: true},
		},
		{
			name:   "delete",
			events: []krt.Event[*networking.IPAddress]{{Event: controllers.EventDelete, Old: &address}},
			used:   map[netip.Addr]bool{v4: true},
			want:   map[netip.Addr]bool{},
		},
		{
			name: "invalid addresses do not stop the batch",
			events: []krt.Event[*networking.IPAddress]{
				{Event: controllers.EventAdd, New: &invalid},
				{Event: controllers.EventUpdate, Old: &invalid, New: &invalid},
				{Event: controllers.EventDelete, Old: &invalid},
				{Event: controllers.EventAdd, New: &v6Address},
			},
			want: map[netip.Addr]bool{v6: true},
		},
		{
			name: "batch",
			events: []krt.Event[*networking.IPAddress]{
				{Event: controllers.EventAdd, New: &address},
				{Event: controllers.EventUpdate, Old: &address, New: &address},
				{Event: controllers.EventAdd, New: &v6Address},
				{Event: controllers.EventDelete, Old: &address},
			},
			want: map[netip.Addr]bool{v6: true},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ipallocator := &IPAllocator{usedAddresses: make(map[netip.Addr]bool)}
			for addr, used := range tt.used {
				ipallocator.usedAddresses[addr] = used
			}
			assert.NoError(t, ipallocator.reconcile(&tt.events))
			assert.Equal(t, ipallocator.usedAddresses, tt.want)
		})
	}
}

func TestIPAllocatorReconcileGlobalService(t *testing.T) {
	service := GlobalService{
		service:      types.NamespacedName{Namespace: "default", Name: "httpbin"},
		network:      "remote-network",
		serviceScope: model.Global,
	}
	localScope := service
	localScope.serviceScope = model.Local
	localNetwork := service
	localNetwork.network = "local-network"
	otherService := service
	otherService.service.Name = "other"
	for _, tt := range []struct {
		name          string
		events        []krt.Event[GlobalService]
		wantAddresses int
	}{
		{
			name:          "add remote global service",
			events:        []krt.Event[GlobalService]{{Event: controllers.EventAdd, New: &service}},
			wantAddresses: 1,
		},
		{
			name:   "ignore local scope",
			events: []krt.Event[GlobalService]{{Event: controllers.EventAdd, New: &localScope}},
		},
		{
			name:   "ignore local network",
			events: []krt.Event[GlobalService]{{Event: controllers.EventAdd, New: &localNetwork}},
		},
		{
			name:          "update local to global scope",
			events:        []krt.Event[GlobalService]{{Event: controllers.EventUpdate, Old: &localScope, New: &service}},
			wantAddresses: 1,
		},
		{
			name:   "update global to local scope",
			events: []krt.Event[GlobalService]{{Event: controllers.EventUpdate, Old: &service, New: &localScope}},
		},
		{
			name:   "ignore update from local network",
			events: []krt.Event[GlobalService]{{Event: controllers.EventUpdate, Old: &localNetwork, New: &service}},
		},
		{
			name:   "delete",
			events: []krt.Event[GlobalService]{{Event: controllers.EventDelete, Old: &service}},
		},
		{
			name: "batch",
			events: []krt.Event[GlobalService]{
				{Event: controllers.EventAdd, New: &localNetwork},
				{Event: controllers.EventAdd, New: &service},
				{Event: controllers.EventUpdate, Old: &localScope, New: &otherService},
			},
			wantAddresses: 2,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			client := kube.NewFakeClient()
			t.Cleanup(client.Shutdown)
			stop := test.NewStop(t)
			addresses := krt.NewStaticCollection[*networking.IPAddress](nil, nil, krt.WithStop(stop))
			ipallocator := &IPAllocator{
				ipAddressWriter:     kclient.NewWriteClient[*networking.IPAddress](client),
				v4Allocator:         allocator{cidrIndex: make(map[netip.Prefix]int)},
				usedAddresses:       make(map[netip.Addr]bool),
				localClusterNetwork: krt.NewStatic(&clusterNetwork{network: "local-network"}, true, krt.WithStop(stop)),
				serviceAddressIndex: krt.NewIndex(addresses, "serviceAddressIndex", func(*networking.IPAddress) []GlobalService {
					return nil
				}),
			}
			ipallocator.addServiceCIDR(&networking.ServiceCIDR{
				Spec: networking.ServiceCIDRSpec{CIDRs: []string{"10.0.0.0/24"}},
			})
			assert.NoError(t, ipallocator.reconcile(&tt.events))
			allocated, err := client.Kube().NetworkingV1beta1().IPAddresses().List(context.Background(), metav1.ListOptions{})
			assert.NoError(t, err)
			assert.Equal(t, len(allocated.Items), tt.wantAddresses)
			assert.Equal(t, len(ipallocator.usedAddresses), tt.wantAddresses)
		})
	}
}

func TestIPAllocatorRun(t *testing.T) {
	stop := make(chan struct{})
	address := &networking.IPAddress{ObjectMeta: metav1.ObjectMeta{Name: "10.0.0.1"}}
	client := kube.NewFakeClient(address.DeepCopy())
	t.Cleanup(client.Shutdown)
	service := GlobalService{
		service:      types.NamespacedName{Namespace: "default", Name: "httpbin"},
		network:      "remote-network",
		serviceScope: model.Global,
	}
	addresses := krt.NewStaticCollection(nil, []*networking.IPAddress{address}, krt.WithStop(stop))
	ipallocator := &IPAllocator{
		ipAddressCollection: addresses,
		ipAddressWriter:     kclient.NewWriteClient[*networking.IPAddress](client),
		serviceCidrCollection: krt.NewStaticCollection(nil, []*networking.ServiceCIDR{{
			Spec: networking.ServiceCIDRSpec{CIDRs: []string{"10.0.0.0/24"}},
		}}, krt.WithStop(stop)),
		v4Allocator:         allocator{cidrIndex: make(map[netip.Prefix]int)},
		v6Allocator:         allocator{cidrIndex: make(map[netip.Prefix]int)},
		usedAddresses:       make(map[netip.Addr]bool),
		globalServices:      krt.NewStaticCollection(nil, []GlobalService{service}, krt.WithStop(stop)),
		localClusterNetwork: krt.NewStatic(&clusterNetwork{network: "local-network"}, true, krt.WithStop(stop)),
		serviceAddressIndex: krt.NewIndex(addresses, "serviceAddressIndex", func(*networking.IPAddress) []GlobalService {
			return nil
		}),
	}
	ipallocator.queue = controllers.NewQueue("ambient-ipallocator", controllers.WithGenericReconciler(ipallocator.reconcile))
	events := []krt.Event[GlobalService]{{Event: controllers.EventAdd, New: &service}}
	ipallocator.queue.Add(&events)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ipallocator.Run(stop)
	}()
	t.Cleanup(func() {
		close(stop)
		<-done
	})
	assert.EventuallyEqual(t, func() bool {
		_, err := client.Kube().NetworkingV1beta1().IPAddresses().Get(context.Background(), "10.0.0.2", metav1.GetOptions{})
		return err == nil
	}, true)
	assert.EventuallyEqual(t, ipallocator.queue.HasSynced, true)
}

func TestIPAllocatorRelease(t *testing.T) {
	address := &networking.IPAddress{ObjectMeta: metav1.ObjectMeta{Name: "10.0.0.42"}}
	client := kube.NewFakeClient(address.DeepCopy())
	service := GlobalService{
		service: types.NamespacedName{Namespace: "default", Name: "httpbin"},
		network: "remote-network",
	}
	addresses := krt.NewStaticCollection(nil, []*networking.IPAddress{address}, krt.WithStop(test.NewStop(t)))
	allocator := &IPAllocator{
		ipAddressWriter: kclient.NewWriteClient[*networking.IPAddress](client),
		usedAddresses:   map[netip.Addr]bool{netip.MustParseAddr(address.Name): true},
		serviceAddressIndex: krt.NewIndex(addresses, "serviceAddressIndex", func(*networking.IPAddress) []GlobalService {
			return []GlobalService{service}
		}),
	}

	allocator.release(&service)

	remaining, err := client.Kube().NetworkingV1beta1().IPAddresses().List(context.Background(), metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Equal(t, len(remaining.Items), 0)
	assert.Equal(t, len(allocator.usedAddresses), 0)
}

func TestIPAllocatorServiceEvents(t *testing.T) {
	for _, tt := range []struct {
		name             string
		network          string
		initial          []corev1.Service
		updates          []corev1.Service
		deletes          []types.NamespacedName
		initialAddresses []networking.IPAddress
		beforeChanges    []string
		wantAddresses    []string
	}{
		{
			name:    "allocates for a remote global Service",
			network: "remote-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
			wantAddresses: []string{"10.0.0.1"},
		},
		{
			name:    "reuses a previously allocated address",
			network: "remote-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
			initialAddresses: []networking.IPAddress{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "10.0.0.42",
					Labels: map[string]string{
						networking.LabelManagedBy:            "remote-service-controller.istio.io",
						"networking.istio.io/remote-ns":      "default",
						"networking.istio.io/remote-service": "httpbin",
						"networking.istio.io/remote-network": "remote-network",
					},
				},
				Spec: networking.IPAddressSpec{ParentRef: &networking.ParentReference{
					Resource: "services", Namespace: "default", Name: "httpbin",
				}},
			}},
			wantAddresses: []string{"10.0.0.42"},
		},
		{
			name:    "ignores a remote local Service",
			network: "remote-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "httpbin"}}},
		},
		{
			name:    "allocates when a remote Service moves from local to global",
			network: "remote-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "httpbin"}}},
			updates: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
			wantAddresses: []string{"10.0.0.1"},
		},
		{
			name:    "removes the allocated address when a global Service becomes local",
			network: "remote-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
			updates:       []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "httpbin"}}},
			beforeChanges: []string{"10.0.0.1"},
		},
		{
			name:    "removes the allocated address when a global Service is deleted",
			network: "remote-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
			deletes:       []types.NamespacedName{{Namespace: "default", Name: "httpbin"}},
			beforeChanges: []string{"10.0.0.1"},
		},
		{
			name:    "ignores a local-to-global update on the local network",
			network: "local-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{Namespace: "default", Name: "httpbin"}}},
			updates: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
		},
		{
			name:    "ignores a global Service on the local network",
			network: "local-network",
			initial: []corev1.Service{{ObjectMeta: metav1.ObjectMeta{
				Namespace: "default", Name: "httpbin", Labels: map[string]string{"global": "true"},
			}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			namespace := func(network string) *corev1.Namespace {
				return &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
					Name: "istio-system", Labels: map[string]string{"topology.istio.io/network": network},
				}}
			}
			localObjects := []runtime.Object{namespace("local-network"), &networking.ServiceCIDR{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
				Spec:       networking.ServiceCIDRSpec{CIDRs: []string{"10.0.0.0/24"}},
			}}
			for _, address := range tt.initialAddresses {
				localObjects = append(localObjects, address.DeepCopy())
			}
			client := kube.NewFakeClient(localObjects...)
			remoteObjects := []runtime.Object{namespace(tt.network), &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "default"}}}
			for _, service := range tt.initial {
				remoteObjects = append(remoteObjects, service.DeepCopy())
			}
			remote := kube.NewFakeClient(remoteObjects...)
			t.Cleanup(client.Shutdown)
			t.Cleanup(remote.Shutdown)
			stop := test.NewStop(t)
			meshConfig := mesh.DefaultMeshConfig()
			meshConfig.ServiceScopeConfigs = []*meshapi.MeshConfig_ServiceScopeConfigs{{
				ServicesSelector: &meshapi.LabelSelector{MatchLabels: map[string]string{"global": "true"}},
				Scope:            meshapi.MeshConfig_ServiceScopeConfigs_GLOBAL,
			}}
			watcher := meshwatcher.NewTestWatcher(meshConfig)
			mc := multicluster.NewController(multicluster.ControllerOptions{
				Client: client, ClusterID: "local", SystemNamespace: "istio-system", MeshConfig: watcher,
				ClientBuilder: func([]byte, cluster.ID, ...func(*rest.Config)) (kube.Client, error) {
					return remote, nil
				},
			})
			allocator := NewIPAllocator(client, stop, mc, watcher)
			assert.NoError(t, mc.Run(stop))
			client.RunAndWait(stop)
			go allocator.Run(stop)
			assert.EventuallyEqual(t, allocator.queue.HasSynced, true)
			assert.EventuallyEqual(t, allocator.globalServices.HasSynced, true)
			_, err := client.Kube().CoreV1().Secrets("istio-system").Create(context.Background(),
				makeSecret("istio-system", "remote", clusterCredential{clusterID: "remote", kubeconfig: []byte("remote")}), metav1.CreateOptions{})
			assert.NoError(t, err)
			t.Cleanup(func() {
				assert.NoError(t, client.Kube().CoreV1().Secrets("istio-system").Delete(context.Background(), "remote", metav1.DeleteOptions{}))
				assert.EventuallyEqual(t, func() bool { return mc.GetRemoteKubeClient("remote") == nil }, true)
			})
			assert.EventuallyEqual(t, func() int { return len(allocator.globalServices.List()) }, len(tt.initial))
			addressNames := func() []string {
				addresses, err := client.Kube().NetworkingV1beta1().IPAddresses().List(context.Background(), metav1.ListOptions{})
				assert.NoError(t, err)
				var names []string
				for _, address := range addresses.Items {
					names = append(names, address.Name)
				}
				return names
			}
			if len(tt.updates) > 0 || len(tt.deletes) > 0 {
				initialScopes := make(map[types.NamespacedName]model.ServiceScope)
				for _, service := range tt.initial {
					scope := model.Local
					if service.Labels["global"] == "true" {
						scope = model.Global
					}
					initialScopes[types.NamespacedName{Namespace: service.Namespace, Name: service.Name}] = scope
				}
				assert.EventuallyEqual(t, func() map[types.NamespacedName]model.ServiceScope {
					scopes := make(map[types.NamespacedName]model.ServiceScope)
					for _, service := range allocator.globalServices.List() {
						scopes[service.service] = service.serviceScope
					}
					return scopes
				}, initialScopes)
				initialNames := tt.beforeChanges
				if initialNames == nil {
					for _, address := range tt.initialAddresses {
						initialNames = append(initialNames, address.Name)
					}
				}
				assert.EventuallyEqual(t, addressNames, initialNames, retry.Converge(5))
			}
			for _, update := range tt.updates {
				service, err := remote.Kube().CoreV1().Services(update.Namespace).Get(context.Background(), update.Name, metav1.GetOptions{})
				assert.NoError(t, err)
				service.Labels = update.Labels
				_, err = remote.Kube().CoreV1().Services(update.Namespace).Update(context.Background(), service, metav1.UpdateOptions{})
				assert.NoError(t, err)
				wantScope := model.Local
				if update.Labels["global"] == "true" {
					wantScope = model.Global
				}
				assert.EventuallyEqual(t, func() bool {
					for _, current := range allocator.globalServices.List() {
						if current.service.Namespace == update.Namespace && current.service.Name == update.Name {
							return current.serviceScope == wantScope
						}
					}
					return false
				}, true)
			}
			for _, service := range tt.deletes {
				assert.NoError(t, remote.Kube().CoreV1().Services(service.Namespace).Delete(context.Background(), service.Name, metav1.DeleteOptions{}))
				assert.EventuallyEqual(t, func() bool {
					for _, current := range allocator.globalServices.List() {
						if current.service == service {
							return false
						}
					}
					return true
				}, true)
			}
			assert.EventuallyEqual(t, addressNames, tt.wantAddresses, retry.Converge(5))
		})
	}
}

func TestIPAllocatorAllocateMissingCIDR(t *testing.T) {
	client := kube.NewFakeClient()
	ipallocator := &IPAllocator{
		ipAddressWriter: kclient.NewWriteClient[*networking.IPAddress](client),
		usedAddresses:   make(map[netip.Addr]bool),
	}
	ipallocator.allocate(&GlobalService{})

	addresses, err := client.Kube().NetworkingV1beta1().IPAddresses().List(context.Background(), metav1.ListOptions{})
	assert.NoError(t, err)
	assert.Equal(t, len(addresses.Items), 0)
	assert.Equal(t, len(ipallocator.usedAddresses), 0)
}

func TestIPAllocatorAllocate(t *testing.T) {
	address := func(ip string) networking.IPAddress {
		return networking.IPAddress{
			ObjectMeta: metav1.ObjectMeta{Name: ip},
		}
	}
	shortCIDR := netip.MustParsePrefix("10.0.0.0/28")
	var exhaustedAddresses []networking.IPAddress
	for addr := shortCIDR.Addr().Next(); shortCIDR.Contains(addr); addr = addr.Next() {
		exhaustedAddresses = append(exhaustedAddresses, address(addr.String()))
	}
	tests := []struct {
		name         string
		cidrs        []string
		deletedCIDRs []string
		currentCIDR  int
		nextAddress  string
		initial      []networking.IPAddress
		want         []networking.IPAddress
		staleCache   bool
	}{
		{
			name:  "creates next address",
			cidrs: []string{"10.0.0.0/24"},
			want:  []networking.IPAddress{address("10.0.0.1")},
		},
		{
			name:    "exhausted /28 creates no IPAddress",
			cidrs:   []string{shortCIDR.String()},
			initial: exhaustedAddresses,
			want:    exhaustedAddresses,
		},
		{
			name:    "skips used address",
			cidrs:   []string{"10.0.0.0/24"},
			initial: []networking.IPAddress{address("10.0.0.1")},
			want: []networking.IPAddress{
				address("10.0.0.1"),
				address("10.0.0.2"),
			},
		},
		{
			name:    "retries existing IPAddress",
			cidrs:   []string{"10.0.0.0/24"},
			initial: []networking.IPAddress{address("10.0.0.1")},
			want: []networking.IPAddress{
				address("10.0.0.1"),
				address("10.0.0.2"),
			},
			staleCache: true,
		},
		{
			name:    "uses next CIDR when first range is used",
			cidrs:   []string{"10.0.0.0/31", "10.1.0.0/24"},
			initial: []networking.IPAddress{address("10.0.0.1")},
			want: []networking.IPAddress{
				address("10.0.0.1"),
				address("10.1.0.1"),
			},
		},
		{
			name:         "deletes the last CIDR while another remains",
			cidrs:        []string{"10.0.0.0/24", "10.1.0.0/24"},
			deletedCIDRs: []string{"10.1.0.0/24"},
			want:         []networking.IPAddress{address("10.0.0.1")},
		},
		{
			name:         "deletes the active CIDR",
			cidrs:        []string{"10.0.0.0/24", "10.1.0.0/24"},
			deletedCIDRs: []string{"10.0.0.0/24"},
			want:         []networking.IPAddress{address("10.1.0.1")},
		},
		{
			name:         "preserves the cursor when the active CIDR moves",
			cidrs:        []string{"10.0.0.0/24", "10.1.0.0/24", "10.2.0.0/24"},
			deletedCIDRs: []string{"10.0.0.0/24"},
			currentCIDR:  2,
			nextAddress:  "10.2.0.5",
			want:         []networking.IPAddress{address("10.2.0.5")},
		},
		{
			name:         "deletes the active CIDR after its cursor leaves the range",
			cidrs:        []string{"10.0.0.0/24", "10.1.0.0/24", "10.2.0.0/24"},
			deletedCIDRs: []string{"10.0.0.0/24"},
			nextAddress:  "10.1.0.5",
			want:         []networking.IPAddress{address("10.2.0.1")},
		},
		{
			name:         "repeated and unknown CIDR deletions are harmless",
			cidrs:        []string{"10.0.0.0/24", "10.1.0.0/24", "10.2.0.0/24"},
			deletedCIDRs: []string{"10.1.0.0/24", "10.1.0.0/24", "10.3.0.0/24", "10.2.0.0/24"},
			want:         []networking.IPAddress{address("10.0.0.1")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var objects []runtime.Object
			for _, initial := range tt.initial {
				objects = append(objects, initial.DeepCopy())
			}
			client := kube.NewFakeClient(objects...)
			ipallocator := &IPAllocator{
				ipAddressWriter: kclient.NewWriteClient[*networking.IPAddress](client),
				usedAddresses:   make(map[netip.Addr]bool),
			}
			ipallocator.v4Allocator = allocator{
				cidrIndex: make(map[netip.Prefix]int),
			}
			for _, cidr := range tt.cidrs {
				ipallocator.v4Allocator.add(netip.MustParsePrefix(cidr))
			}
			if !tt.staleCache {
				for _, initial := range tt.initial {
					ipallocator.usedAddresses[netip.MustParseAddr(initial.Name)] = true
				}
			}
			ipallocator.v4Allocator.currentCidr = tt.currentCIDR
			if tt.nextAddress != "" {
				ipallocator.v4Allocator.nextAddress = netip.MustParseAddr(tt.nextAddress)
			}
			ipallocator.removeServiceCIDR(&networking.ServiceCIDR{
				Spec: networking.ServiceCIDRSpec{CIDRs: tt.deletedCIDRs},
			})
			assert.Equal(t, len(ipallocator.v4Allocator.cidrIndex), len(ipallocator.v4Allocator.cidrs))
			for index, cidr := range ipallocator.v4Allocator.cidrs {
				actualIndex, exists := ipallocator.v4Allocator.cidrIndex[cidr]
				assert.Equal(t, exists, true)
				assert.Equal(t, actualIndex, index)
			}
			for _, cidr := range tt.deletedCIDRs {
				_, exists := ipallocator.v4Allocator.cidrIndex[netip.MustParsePrefix(cidr)]
				assert.Equal(t, exists, false)
			}

			service := &GlobalService{
				service: types.NamespacedName{Namespace: "default", Name: "httpbin"},
				network: "remote-network",
			}
			ipallocator.allocate(service)

			addresses, err := client.Kube().NetworkingV1beta1().IPAddresses().List(context.Background(), metav1.ListOptions{})
			assert.NoError(t, err)
			got := make(map[string]networking.IPAddress)
			for _, actual := range addresses.Items {
				actual.TypeMeta = metav1.TypeMeta{}
				actual.ManagedFields = nil
				got[actual.Name] = actual
			}
			want := make(map[string]networking.IPAddress)
			initialNames := make(map[string]bool)
			for _, initial := range tt.initial {
				initialNames[initial.Name] = true
			}
			for _, expected := range tt.want {
				if !initialNames[expected.Name] {
					expected.Labels = map[string]string{
						networking.LabelManagedBy:            "remote-service-controller.istio.io",
						"networking.istio.io/remote-ns":      "default",
						"networking.istio.io/remote-service": "httpbin",
						"networking.istio.io/remote-network": "remote-network",
					}
					expected.Spec.ParentRef = &networking.ParentReference{
						Group:     "",
						Resource:  "services",
						Namespace: "default",
						Name:      "httpbin",
					}
				}
				want[expected.Name] = expected
			}
			assert.Equal(t, got, want)
			for _, expected := range tt.want {
				assert.Equal(t, ipallocator.usedAddresses[netip.MustParseAddr(expected.Name)], true)
			}
		})
	}
}
