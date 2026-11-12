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
	"istio.io/istio/pkg/kube/kclient"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/assert"
	"istio.io/istio/pkg/test/util/retry"
)

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
			assert.EventuallyEqual(t, func() int { return len(allocator.v4Allocator.cidrs) }, 1)
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
