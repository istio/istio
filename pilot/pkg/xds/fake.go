//go:build !agent
// +build !agent

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
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/kubernetes/scheme"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/autoregistration"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	"istio.io/istio/pilot/pkg/config/kube/ingress"
	kubesecrets "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/serviceregistry"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
)

type FakeOptions struct {
	// If provided, sets the name of the "default" or local cluster to the similaed pilots. (Defaults to opts.DefaultClusterName)
	DefaultClusterName cluster.ID
	// If provided, the minor version will be overridden for calls to GetKubernetesVersion to 1.minor
	KubernetesVersion string
	// If provided, a service registry with the name of each map key will be created with the given objects.
	KubernetesObjectsByCluster map[cluster.ID][]runtime.Object
	// If provided, these objects will be used directly for the default cluster ("Kubernetes" or DefaultClusterName)
	KubernetesObjects []runtime.Object
	// If provided, a service registry with the name of each map key will be created with the given objects.
	KubernetesObjectStringByCluster map[cluster.ID]string
	// If provided, the yaml string will be parsed and used as objects for the default cluster ("Kubernetes" or DefaultClusterName)
	KubernetesObjectString string
	// Endpoint mode for the Kubernetes service registry
	KubernetesEndpointMode kube.EndpointMode
	// If provided, these configs will be used directly
	Configs []config.Config
	// If provided, the yaml string will be parsed and used as configs
	ConfigString string
	// If provided, the ConfigString will be treated as a go template, with this as input params
	ConfigTemplateInput interface{}
	// If provided, this mesh config will be used
	MeshConfig      *meshconfig.MeshConfig
	NetworksWatcher mesh.NetworksWatcher

	// Callback to modify the server before it is started
	DiscoveryServerModifier func(s *DiscoveryServer)
	// Callback to modify the kube client before it is started
	KubeClientModifier func(c kubelib.Client)

	// ListenerBuilder, if specified, allows making the server use the given
	// listener instead of a buffered conn.
	ListenerBuilder func() (net.Listener, error)

	// Time to debounce
	// By default, set to 0s to speed up tests
	DebounceTime time.Duration

	// EnableFakeXDSUpdater will use a XDSUpdater that can be used to watch events
	EnableFakeXDSUpdater       bool
	DisableSecretAuthorization bool
	Services                   []*model.Service
	Gateways                   []model.NetworkGateway
}

type FakeDiscoveryServer struct {
	*v1alpha3.ConfigGenTest
	t            test.Failer
	Discovery    *DiscoveryServer
	Listener     net.Listener
	BufListener  *bufconn.Listener
	kubeClient   kubelib.Client
	KubeRegistry *kube.FakeController
	XdsUpdater   model.XDSUpdater
}

func NewFakeDiscoveryServer(t test.Failer, opts FakeOptions) *FakeDiscoveryServer {
	stop := test.NewStop(t)

	m := opts.MeshConfig
	if m == nil {
		m = mesh.DefaultMeshConfig()
	}

	// Init with a dummy environment, since we have a circular dependency with the env creation.
	s := NewDiscoveryServer(&model.Environment{PushContext: model.NewPushContext()}, "pilot-123", map[string]string{})
	s.InitGenerators(s.Env, "istio-system")
	t.Cleanup(func() {
		s.JwtKeyResolver.Close()
		s.pushQueue.ShutDown()
	})

	serviceHandler := func(svc *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      gvk.ServiceEntry,
				Name:      string(svc.Hostname),
				Namespace: svc.Attributes.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ServiceUpdate},
		}
		s.ConfigUpdate(pushReq)
	}

	if opts.DefaultClusterName == "" {
		opts.DefaultClusterName = "Kubernetes"
	}
	k8sObjects := getKubernetesObjects(t, opts)
	var defaultKubeClient kubelib.Client
	var defaultKubeController *kube.FakeController
	var registries []serviceregistry.Instance
	if opts.NetworksWatcher != nil {
		opts.NetworksWatcher.AddNetworksHandler(func() {
			s.ConfigUpdate(&model.PushRequest{
				Full:   true,
				Reason: []model.TriggerReason{model.NetworksTrigger},
			})
		})
	}
	var xdsUpdater model.XDSUpdater = s
	if opts.EnableFakeXDSUpdater {
		evChan := make(chan FakeXdsEvent, 1000)
		xdsUpdater = &FakeXdsUpdater{
			Events:   evChan,
			Delegate: s,
		}
	}
	creds := kubesecrets.NewMulticluster(opts.DefaultClusterName)
	s.Generators[v3.SecretType] = NewSecretGen(creds, s.Cache, opts.DefaultClusterName, nil)
	s.Generators[v3.ExtensionConfigurationType].(*EcdsGenerator).SetCredController(creds)
	for k8sCluster, objs := range k8sObjects {
		client := kubelib.NewFakeClientWithVersion(opts.KubernetesVersion, objs...)
		if opts.KubeClientModifier != nil {
			opts.KubeClientModifier(client)
		}
		k8s, _ := kube.NewFakeControllerWithOptions(t, kube.FakeControllerOptions{
			ServiceHandler:  serviceHandler,
			Client:          client,
			ClusterID:       k8sCluster,
			DomainSuffix:    "cluster.local",
			XDSUpdater:      xdsUpdater,
			NetworksWatcher: opts.NetworksWatcher,
			Mode:            opts.KubernetesEndpointMode,
			Stop:            stop,
		})
		// start default client informers after creating ingress/secret controllers
		if defaultKubeClient == nil || k8sCluster == opts.DefaultClusterName {
			defaultKubeClient = client
			defaultKubeController = k8s
		} else {
			client.RunAndWait(stop)
		}
		registries = append(registries, k8s)
		if err := creds.ClusterAdded(&multicluster.Cluster{ID: k8sCluster, Client: client}, nil); err != nil {
			t.Fatal(err)
		}
	}

	if opts.DisableSecretAuthorization {
		kubesecrets.DisableAuthorizationForTest(defaultKubeClient.Kube().(*fake.Clientset))
	}
	ingr := ingress.NewController(defaultKubeClient, mesh.NewFixedWatcher(m), kube.Options{
		DomainSuffix: "cluster.local",
	})
	defaultKubeClient.RunAndWait(stop)

	var gwc *gateway.Controller
	cg := v1alpha3.NewConfigGenTest(t, v1alpha3.TestOptions{
		Configs:             opts.Configs,
		ConfigString:        opts.ConfigString,
		ConfigTemplateInput: opts.ConfigTemplateInput,
		MeshConfig:          opts.MeshConfig,
		NetworksWatcher:     opts.NetworksWatcher,
		ServiceRegistries:   registries,
		PushContextLock:     &s.updateMutex,
		ConfigStoreCaches:   []model.ConfigStoreController{ingr},
		CreateConfigStore: func(c model.ConfigStoreController) model.ConfigStoreController {
			g := gateway.NewController(defaultKubeClient, c, kube.Options{
				DomainSuffix: "cluster.local",
			})
			gwc = g
			return gwc
		},
		SkipRun:   true,
		ClusterID: defaultKubeController.Cluster(),
		Services:  opts.Services,
		Gateways:  opts.Gateways,
	})
	cg.ServiceEntryRegistry.AppendServiceHandler(serviceHandler)
	s.updateMutex.Lock()
	s.Env = cg.Env()
	s.Env.GatewayAPIController = gwc
	if err := s.Env.InitNetworksManager(s); err != nil {
		t.Fatal(err)
	}
	// Disable debounce to reduce test times
	s.debounceOptions.debounceAfter = opts.DebounceTime
	s.MemRegistry = cg.MemRegistry
	s.MemRegistry.EDSUpdater = s
	s.updateMutex.Unlock()

	// Setup config handlers
	// TODO code re-use from server.go
	configHandler := func(_, curr config.Config, event model.Event) {
		pushReq := &model.PushRequest{
			Full: true,
			ConfigsUpdated: map[model.ConfigKey]struct{}{{
				Kind:      curr.GroupVersionKind,
				Name:      curr.Name,
				Namespace: curr.Namespace,
			}: {}},
			Reason: []model.TriggerReason{model.ConfigUpdate},
		}
		s.ConfigUpdate(pushReq)
	}
	schemas := collections.Pilot.All()
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI.All()
	}
	for _, schema := range schemas {
		// This resource type was handled in external/servicediscovery.go, no need to rehandle here.
		if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Serviceentries.
			Resource().GroupVersionKind() {
			continue
		}
		if schema.Resource().GroupVersionKind() == collections.IstioNetworkingV1Alpha3Workloadentries.
			Resource().GroupVersionKind() {
			continue
		}

		cg.Store().RegisterEventHandler(schema.Resource().GroupVersionKind(), configHandler)
	}
	for _, registry := range registries {
		k8s, ok := registry.(*kube.FakeController)
		// this closely matches what we do in serviceregistry/kube/controller/multicluster.go
		if !ok || k8s.Cluster() != cg.ServiceEntryRegistry.Cluster() {
			continue
		}
		cg.ServiceEntryRegistry.AppendWorkloadHandler(k8s.WorkloadInstanceHandler)
		k8s.AppendWorkloadHandler(cg.ServiceEntryRegistry.WorkloadInstanceHandler)
	}
	s.WorkloadEntryController = autoregistration.NewController(cg.Store(), "test", keepalive.Infinity)

	if opts.DiscoveryServerModifier != nil {
		opts.DiscoveryServerModifier(s)
	}

	var listener net.Listener
	if opts.ListenerBuilder != nil {
		var err error
		if listener, err = opts.ListenerBuilder(); err != nil {
			t.Fatal(err)
		}
	} else {
		// Start in memory gRPC listener
		buffer := 1024 * 1024
		listener = bufconn.Listen(buffer)
	}

	grpcServer := grpc.NewServer()
	s.Register(grpcServer)
	go func() {
		if err := grpcServer.Serve(listener); err != nil && !(err == grpc.ErrServerStopped || err.Error() == "closed") {
			t.Fatal(err)
		}
	}()
	t.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	// Start the discovery server
	s.Start(stop)
	cg.ServiceEntryRegistry.XdsUpdater = s
	// Now that handlers are added, get everything started
	cg.Run()
	kubelib.WaitForCacheSync(stop,
		cg.Registry.HasSynced,
		cg.Store().HasSynced)
	cg.ServiceEntryRegistry.ResyncEDS()

	// Send an update. This ensures that even if there are no configs provided, the push context is
	// initialized.
	s.ConfigUpdate(&model.PushRequest{Full: true})

	processStartTime = time.Now()

	// Wait until initial updates are committed
	c := s.InboundUpdates.Load()
	retry.UntilOrFail(t, func() bool {
		return s.CommittedUpdates.Load() >= c
	}, retry.Delay(time.Millisecond))

	// Mark ourselves ready
	s.CachesSynced()

	bufListener, _ := listener.(*bufconn.Listener)
	fake := &FakeDiscoveryServer{
		t:             t,
		Discovery:     s,
		Listener:      listener,
		BufListener:   bufListener,
		ConfigGenTest: cg,
		kubeClient:    defaultKubeClient,
		KubeRegistry:  defaultKubeController,
		XdsUpdater:    xdsUpdater,
	}

	return fake
}

func (f *FakeDiscoveryServer) KubeClient() kubelib.Client {
	return f.kubeClient
}

func (f *FakeDiscoveryServer) PushContext() *model.PushContext {
	f.Discovery.updateMutex.RLock()
	defer f.Discovery.updateMutex.RUnlock()
	return f.Env().PushContext
}

// ConnectADS starts an ADS connection to the server. It will automatically be cleaned up when the test ends
func (f *FakeDiscoveryServer) ConnectADS() *AdsTest {
	conn, err := grpc.Dial("buffcon",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.BufListener.Dial()
		}))
	if err != nil {
		f.t.Fatalf("failed to connect: %v", err)
	}
	return NewAdsTest(f.t, conn)
}

// ConnectDeltaADS starts a Delta ADS connection to the server. It will automatically be cleaned up when the test ends
func (f *FakeDiscoveryServer) ConnectDeltaADS() *DeltaAdsTest {
	conn, err := grpc.Dial("buffcon",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.BufListener.Dial()
		}))
	if err != nil {
		f.t.Fatalf("failed to connect: %v", err)
	}
	return NewDeltaAdsTest(f.t, conn)
}

// Connect starts an ADS connection to the server using adsc. It will automatically be cleaned up when the test ends
// watch can be configured to determine the resources to watch initially, and wait can be configured to determine what
// resources we should initially wait for.
func (f *FakeDiscoveryServer) Connect(p *model.Proxy, watch []string, wait []string) *adsc.ADSC {
	f.t.Helper()
	p = f.SetupProxy(p)
	initialWatch := []*discovery.DiscoveryRequest{}
	if watch == nil {
		initialWatch = []*discovery.DiscoveryRequest{{TypeUrl: v3.ClusterType}}
	} else {
		for _, typeURL := range watch {
			initialWatch = append(initialWatch, &discovery.DiscoveryRequest{TypeUrl: typeURL})
		}
	}
	if wait == nil {
		initialWatch = []*discovery.DiscoveryRequest{{TypeUrl: v3.ClusterType}}
	}
	adscConn, err := adsc.New("buffcon", &adsc.Config{
		IP:                       p.IPAddresses[0],
		NodeType:                 string(p.Type),
		Meta:                     p.Metadata.ToStruct(),
		Locality:                 p.Locality,
		Namespace:                p.ConfigNamespace,
		InitialDiscoveryRequests: initialWatch,
		GrpcOpts: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
				return f.BufListener.Dial()
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		},
	})
	if err != nil {
		f.t.Fatalf("Error connecting: %v", err)
	}
	if err := adscConn.Run(); err != nil {
		f.t.Fatalf("ADSC: failed running: %v", err)
	}

	if len(wait) > 0 {
		_, err = adscConn.Wait(10*time.Second, wait...)
		if err != nil {
			f.t.Fatalf("Error getting initial for %v config: %v", wait, err)
		}
	}
	f.t.Cleanup(func() {
		adscConn.Close()
	})
	return adscConn
}

func (f *FakeDiscoveryServer) Endpoints(p *model.Proxy) []*endpoint.ClusterLoadAssignment {
	loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
	for _, c := range xdstest.ExtractEdsClusterNames(f.Clusters(p)) {
		loadAssignments = append(loadAssignments, f.Discovery.generateEndpoints(NewEndpointBuilder(c, p, f.PushContext())))
	}
	return loadAssignments
}

func getKubernetesObjects(t test.Failer, opts FakeOptions) map[cluster.ID][]runtime.Object {
	objects := map[cluster.ID][]runtime.Object{}

	if len(opts.KubernetesObjects) > 0 {
		objects[opts.DefaultClusterName] = append(objects[opts.DefaultClusterName], opts.KubernetesObjects...)
	}
	if len(opts.KubernetesObjectString) > 0 {
		parsed, err := kubernetesObjectsFromString(opts.KubernetesObjectString)
		if err != nil {
			t.Fatalf("failed parsing KubernetesObjectString: %v", err)
		}
		objects[opts.DefaultClusterName] = append(objects[opts.DefaultClusterName], parsed...)
	}
	for k8sCluster, objectStr := range opts.KubernetesObjectStringByCluster {
		parsed, err := kubernetesObjectsFromString(objectStr)
		if err != nil {
			t.Fatalf("failed parsing KubernetesObjectStringByCluster for %s: %v", k8sCluster, err)
		}
		objects[k8sCluster] = append(objects[k8sCluster], parsed...)
	}
	for k8sCluster, clusterObjs := range opts.KubernetesObjectsByCluster {
		objects[k8sCluster] = append(objects[k8sCluster], clusterObjs...)
	}

	if len(objects) == 0 {
		return map[cluster.ID][]runtime.Object{opts.DefaultClusterName: {}}
	}

	return objects
}

func kubernetesObjectsFromString(s string) ([]runtime.Object, error) {
	var objects []runtime.Object
	decode := scheme.Codecs.UniversalDeserializer().Decode
	objectStrs := strings.Split(s, "---")
	for _, s := range objectStrs {
		if len(strings.TrimSpace(s)) == 0 {
			continue
		}
		o, _, err := decode([]byte(s), nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed deserializing kubernetes object: %v", err)
		}
		objects = append(objects, o)
	}
	return objects, nil
}

type FakeXdsEvent struct {
	Kind      string
	Host      string
	Namespace string
	Endpoints int
	PushReq   *model.PushRequest
}

type FakeXdsUpdater struct {
	// Events tracks notifications received by the updater
	Events   chan FakeXdsEvent
	Delegate model.XDSUpdater
}

var _ model.XDSUpdater = &FakeXdsUpdater{}

func (fx *FakeXdsUpdater) EDSUpdate(s model.ShardKey, hostname string, namespace string, entry []*model.IstioEndpoint) {
	fx.Events <- FakeXdsEvent{Kind: "eds", Host: hostname, Namespace: namespace, Endpoints: len(entry)}
	if fx.Delegate != nil {
		fx.Delegate.EDSUpdate(s, hostname, namespace, entry)
	}
}

func (fx *FakeXdsUpdater) EDSCacheUpdate(s model.ShardKey, hostname string, namespace string, entry []*model.IstioEndpoint) {
	fx.Events <- FakeXdsEvent{Kind: "edscache", Host: hostname, Namespace: namespace, Endpoints: len(entry)}
	if fx.Delegate != nil {
		fx.Delegate.EDSCacheUpdate(s, hostname, namespace, entry)
	}
}

func (fx *FakeXdsUpdater) ConfigUpdate(req *model.PushRequest) {
	fx.Events <- FakeXdsEvent{Kind: "xds", PushReq: req}
	if fx.Delegate != nil {
		fx.Delegate.ConfigUpdate(req)
	}
}

func (fx *FakeXdsUpdater) ProxyUpdate(c cluster.ID, p string) {
	if fx.Delegate != nil {
		fx.Delegate.ProxyUpdate(c, p)
	}
}

func (fx *FakeXdsUpdater) SvcUpdate(s model.ShardKey, hostname string, namespace string, e model.Event) {
	fx.Events <- FakeXdsEvent{Kind: "svcupdate", Host: hostname, Namespace: namespace}
	if fx.Delegate != nil {
		fx.Delegate.SvcUpdate(s, hostname, namespace, e)
	}
}

func (fx *FakeXdsUpdater) RemoveShard(_ model.ShardKey) {
	fx.Events <- FakeXdsEvent{Kind: "removeshard"}
	fx.ConfigUpdate(&model.PushRequest{Full: true})
}

func (fx *FakeXdsUpdater) WaitDurationOrFail(t test.Failer, duration time.Duration, types ...string) *FakeXdsEvent {
	t.Helper()
	got := fx.WaitDuration(duration, types...)
	if got == nil {
		t.Fatal("missing event")
	}
	return got
}

func (fx *FakeXdsUpdater) WaitOrFail(t test.Failer, types ...string) *FakeXdsEvent {
	t.Helper()
	got := fx.Wait(types...)
	if got == nil {
		t.Fatal("missing event")
	}
	return got
}

func (fx *FakeXdsUpdater) WaitDuration(duration time.Duration, types ...string) *FakeXdsEvent {
	for {
		select {
		case e := <-fx.Events:
			for _, et := range types {
				if e.Kind == et {
					return &e
				}
			}
			continue
		case <-time.After(duration):
			return nil
		}
	}
}

func (fx *FakeXdsUpdater) Wait(types ...string) *FakeXdsEvent {
	return fx.WaitDuration(1*time.Second, types...)
}
