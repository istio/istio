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
	"net/http"
	"strings"
	"time"

	endpoint "github.com/envoyproxy/go-control-plane/envoy/config/endpoint/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	authorizationv1 "k8s.io/api/authorization/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/autoregistration"
	"istio.io/istio/pilot/pkg/bootstrap"
	"istio.io/istio/pilot/pkg/config/kube/gateway"
	ingress "istio.io/istio/pilot/pkg/config/kube/ingress"
	"istio.io/istio/pilot/pkg/config/memory"
	kubesecrets "istio.io/istio/pilot/pkg/credentials/kube"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry"
	kube "istio.io/istio/pilot/pkg/serviceregistry/kube/controller"
	memregistry "istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/util/xdsfake"
	"istio.io/istio/pilot/pkg/xds"
	"istio.io/istio/pilot/pkg/xds/endpoints"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pilot/test/xdstest"
	"istio.io/istio/pkg/adsc"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/schema/collections"
	"istio.io/istio/pkg/config/schema/gvk"
	"istio.io/istio/pkg/config/schema/gvr"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/keepalive"
	kubelib "istio.io/istio/pkg/kube"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/multicluster"
	"istio.io/istio/pkg/test"
	"istio.io/istio/pkg/test/util/retry"
	"istio.io/istio/pkg/util/sets"
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
	// If provided, these configs will be used directly
	Configs []config.Config
	// If provided, the yaml string will be parsed and used as configs
	ConfigString string
	// If provided, the ConfigString will be treated as a go template, with this as input params
	ConfigTemplateInput any
	// If provided, this mesh config will be used
	MeshConfig      *meshconfig.MeshConfig
	NetworksWatcher mesh.NetworksWatcher

	// Callback to modify the kube client before it is started
	KubeClientModifier func(c kubelib.Client)

	// Override the default kube client constructor
	KubeClientBuilder func(objects ...runtime.Object) kubelib.Client

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
	*core.ConfigGenTest
	t              test.Failer
	Discovery      *xds.DiscoveryServer
	DiscoveryDebug *http.ServeMux
	Listener       net.Listener
	BufListener    *bufconn.Listener
	kubeClient     kubelib.Client
	KubeRegistry   *kube.FakeController
	XdsUpdater     model.XDSUpdater
	MemRegistry    *memregistry.ServiceDiscovery
}

func NewFakeDiscoveryServer(t test.Failer, opts FakeOptions) *FakeDiscoveryServer {
	m := opts.MeshConfig
	if m == nil {
		m = mesh.DefaultMeshConfig()
	}

	// Init with a dummy environment, since we have a circular dependency with the env creation.
	s := xds.NewDiscoveryServer(model.NewEnvironment(), map[cluster.ID]cluster.ID{}, krt.GlobalDebugHandler)
	// Disable debounce to reduce test times
	s.DebounceOptions.DebounceAfter = opts.DebounceTime
	// Setup time to Now instead of process start to make logs not misleading
	s.DiscoveryStartTime = time.Now()
	t.Cleanup(s.Shutdown)

	serviceHandler := func(_, curr *model.Service, _ model.Event) {
		pushReq := &model.PushRequest{
			Full:           true,
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: kind.ServiceEntry, Name: string(curr.Hostname), Namespace: curr.Attributes.Namespace}),
			Reason:         model.NewReasonStats(model.ServiceUpdate),
		}
		s.ConfigUpdate(pushReq)
	}

	if opts.DefaultClusterName == "" {
		opts.DefaultClusterName = constants.DefaultClusterName
	}
	k8sObjects := getKubernetesObjects(t, opts)
	var defaultKubeClient kubelib.Client
	var defaultKubeController *kube.FakeController
	var registries []serviceregistry.Instance
	if opts.NetworksWatcher != nil {
		opts.NetworksWatcher.AddNetworksHandler(func() {
			s.ConfigUpdate(&model.PushRequest{
				Full:   true,
				Reason: model.NewReasonStats(model.NetworksTrigger),
				Forced: true,
			})
		})
	}
	var xdsUpdater model.XDSUpdater = s
	if opts.EnableFakeXDSUpdater {
		xdsUpdater = xdsfake.NewWithDelegate(s)
	}
	mc := multicluster.NewFakeController()
	creds := kubesecrets.NewMulticluster(opts.DefaultClusterName, mc)

	configController := memory.NewSyncController(memory.MakeSkipValidation(collections.PilotGatewayAPI()))
	clientBuilder := opts.KubeClientBuilder
	if clientBuilder == nil {
		clientBuilder = func(objects ...runtime.Object) kubelib.Client {
			return kubelib.NewFakeClientWithVersion(opts.KubernetesVersion, objects...)
		}
	}
	for k8sCluster, objs := range k8sObjects {
		client := clientBuilder(objs...)
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
			SkipRun:         true,
			ConfigCluster:   k8sCluster == opts.DefaultClusterName,
			MeshWatcher:     meshwatcher.NewTestWatcher(m),
			CRDs: []schema.GroupVersionResource{
				// Install all CRDs used (mostly in Ambient)
				gvr.AuthorizationPolicy,
				gvr.PeerAuthentication,
				gvr.WorkloadEntry,
				gvr.GatewayClass,
				gvr.KubernetesGateway,
				gvr.HTTPRoute,
				gvr.GRPCRoute,
				gvr.TCPRoute,
				gvr.TLSRoute,
				gvr.ReferenceGrant,
				gvr.ServiceEntry,
			},
		})
		stop := test.NewStop(t)
		// start default client informers after creating ingress/secret controllers
		if defaultKubeClient == nil || k8sCluster == opts.DefaultClusterName {
			defaultKubeClient = client
			if opts.DisableSecretAuthorization {
				DisableAuthorizationForSecret(defaultKubeClient.Kube().(*fake.Clientset))
			}
			defaultKubeController = k8s
		} else {
			client.RunAndWait(stop)
		}
		registries = append(registries, k8s)
		mc.Add(k8sCluster, client, stop)
	}

	stop := test.NewStop(t)
	ingr := ingress.NewController(defaultKubeClient, meshwatcher.NewTestWatcher(m), kube.Options{
		DomainSuffix: "cluster.local",
	}, xdsUpdater)
	defaultKubeClient.RunAndWait(stop)

	var gwc *gateway.Controller
	cg := core.NewConfigGenTest(t, core.TestOptions{
		Configs:             opts.Configs,
		ConfigString:        opts.ConfigString,
		ConfigTemplateInput: opts.ConfigTemplateInput,
		ConfigController:    configController,
		MeshConfig:          m,
		XDSUpdater:          xdsUpdater,
		NetworksWatcher:     opts.NetworksWatcher,
		ServiceRegistries:   registries,
		ConfigStoreCaches:   []model.ConfigStoreController{ingr},
		CreateConfigStore: func(c model.ConfigStoreController) model.ConfigStoreController {
			g := gateway.NewController(defaultKubeClient, func(class schema.GroupVersionResource, stop <-chan struct{}) bool {
				return true
			}, kube.Options{
				DomainSuffix: "cluster.local",
			}, xdsUpdater)
			gwc = g
			return gwc
		},
		SkipRun:   true,
		ClusterID: opts.DefaultClusterName,
		Services:  opts.Services,
		Gateways:  opts.Gateways,
	})
	cg.Registry.AppendServiceHandler(serviceHandler)
	s.Env = cg.Env()
	s.Env.GatewayAPIController = gwc
	if err := s.Env.InitNetworksManager(s); err != nil {
		t.Fatal(err)
	}

	bootstrap.InitGenerators(s, core.NewConfigGenerator(s.Cache), "istio-system", "", nil)
	s.Generators[v3.SecretType] = xds.NewSecretGen(creds, s.Cache, opts.DefaultClusterName, nil)
	s.Generators[v3.ExtensionConfigurationType].(*xds.EcdsGenerator).SetCredController(creds)

	debugMux := s.InitDebug(http.NewServeMux(), false, func() map[string]string {
		return nil
	})

	memRegistry := cg.MemRegistry
	memRegistry.XdsUpdater = s

	// Setup config handlers
	// TODO code re-use from server.go
	configHandler := func(_, curr config.Config, event model.Event) {
		pushReq := &model.PushRequest{
			Full:           true,
			ConfigsUpdated: sets.New(model.ConfigKey{Kind: gvk.MustToKind(curr.GroupVersionKind), Name: curr.Name, Namespace: curr.Namespace}),
			Reason:         model.NewReasonStats(model.ConfigUpdate),
		}
		s.ConfigUpdate(pushReq)
	}
	schemas := collections.Pilot.All()
	if features.EnableGatewayAPI {
		schemas = collections.PilotGatewayAPI().All()
	}
	for _, schema := range schemas {
		// This resource type was handled in external/servicediscovery.go, no need to rehandle here.
		if schema.GroupVersionKind() == gvk.ServiceEntry {
			continue
		}
		if schema.GroupVersionKind() == gvk.WorkloadEntry {
			continue
		}

		cg.Store().RegisterEventHandler(schema.GroupVersionKind(), configHandler)
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
	kubelib.WaitForCacheSync("fake", stop,
		cg.Registry.HasSynced,
		cg.Store().HasSynced)
	cg.ServiceEntryRegistry.ResyncEDS()

	// Send an update. This ensures that even if there are no configs provided, the push context is
	// initialized.
	s.ConfigUpdate(&model.PushRequest{Full: true, Forced: true})

	// Wait until initial updates are committed
	c := s.InboundUpdates.Load()
	retry.UntilOrFail(t, func() bool {
		return s.CommittedUpdates.Load() >= c
	}, retry.Delay(time.Millisecond))

	// Mark ourselves ready
	s.CachesSynced()

	bufListener, _ := listener.(*bufconn.Listener)
	fake := &FakeDiscoveryServer{
		t:              t,
		Discovery:      s,
		DiscoveryDebug: debugMux,
		Listener:       listener,
		BufListener:    bufListener,
		ConfigGenTest:  cg,
		kubeClient:     defaultKubeClient,
		KubeRegistry:   defaultKubeController,
		XdsUpdater:     xdsUpdater,
		MemRegistry:    memRegistry,
	}

	return fake
}

func (f *FakeDiscoveryServer) KubeClient() kubelib.Client {
	return f.kubeClient
}

func (f *FakeDiscoveryServer) PushContext() *model.PushContext {
	return f.Env().PushContext()
}

// ConnectADS starts an ADS connection to the server. It will automatically be cleaned up when the test ends
func (f *FakeDiscoveryServer) ConnectADS() *xds.AdsTest {
	conn, err := grpc.Dial("buffcon",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.BufListener.Dial()
		}))
	if err != nil {
		f.t.Fatalf("failed to connect: %v", err)
	}
	return xds.NewAdsTest(f.t, conn)
}

// ConnectDeltaADS starts a Delta ADS connection to the server. It will automatically be cleaned up when the test ends
func (f *FakeDiscoveryServer) ConnectDeltaADS() *xds.DeltaAdsTest {
	conn, err := grpc.Dial("buffcon",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.BufListener.Dial()
		}))
	if err != nil {
		f.t.Fatalf("failed to connect: %v", err)
	}
	return xds.NewDeltaAdsTest(f.t, conn)
}

func APIWatches() []string {
	watches := []string{gvk.MeshConfig.String()}
	for _, sch := range collections.Pilot.All() {
		watches = append(watches, sch.GroupVersionKind().String())
	}
	return watches
}

func (f *FakeDiscoveryServer) ConnectUnstarted(p *model.Proxy, watch []string) *adsc.ADSC {
	f.t.Helper()
	p = f.SetupProxy(p)
	initialWatch := []*discovery.DiscoveryRequest{}
	for _, typeURL := range watch {
		initialWatch = append(initialWatch, &discovery.DiscoveryRequest{TypeUrl: typeURL})
	}
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	if f.BufListener != nil {
		opts = append(opts, grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return f.BufListener.Dial()
		}))
	}
	adscConn, err := adsc.New(f.Listener.Addr().String(), &adsc.ADSConfig{
		Config: adsc.Config{
			IP:        p.IPAddresses[0],
			NodeType:  p.Type,
			Meta:      p.Metadata.ToStruct(),
			Locality:  p.Locality,
			Namespace: p.ConfigNamespace,
			GrpcOpts:  opts,
		},
		InitialDiscoveryRequests: initialWatch,
	})
	if err != nil {
		f.t.Fatalf("Error connecting: %v", err)
	}
	f.t.Cleanup(func() {
		adscConn.Close()
	})
	return adscConn
}

// Connect starts an ADS connection to the server using adsc. It will automatically be cleaned up when the test ends
// watch can be configured to determine the resources to watch initially, and wait can be configured to determine what
// resources we should initially wait for.
func (f *FakeDiscoveryServer) Connect(p *model.Proxy, watch []string, wait []string) *adsc.ADSC {
	f.t.Helper()
	if watch == nil {
		watch = []string{v3.ClusterType}
	}
	adscConn := f.ConnectUnstarted(p, watch)
	if err := adscConn.Run(); err != nil {
		f.t.Fatalf("ADSC: failed running: %v", err)
	}
	if len(wait) > 0 {
		_, err := adscConn.Wait(10*time.Second, wait...)
		if err != nil {
			f.t.Fatalf("Error getting initial for %v config: %v", wait, err)
		}
	}
	return adscConn
}

func (f *FakeDiscoveryServer) Endpoints(p *model.Proxy) []*endpoint.ClusterLoadAssignment {
	loadAssignments := make([]*endpoint.ClusterLoadAssignment, 0)
	for _, c := range xdstest.ExtractEdsClusterNames(f.Clusters(p)) {
		builder := endpoints.NewEndpointBuilder(c, p, f.PushContext())
		loadAssignments = append(loadAssignments, builder.BuildClusterLoadAssignment(f.Discovery.Env.EndpointIndex))
	}
	return loadAssignments
}

func (f *FakeDiscoveryServer) T() test.Failer {
	return f.t
}

// EnsureSynced checks that all ConfigUpdates sent have been established
// This does NOT ensure that the change has been sent to all proxies; only that PushContext is updated
// Typically, if trying to ensure changes are sent, its better to wait for the push event.

func (f *FakeDiscoveryServer) EnsureSynced(t test.Failer) {
	c := f.Discovery.InboundUpdates.Load()
	retry.UntilOrFail(t, func() bool {
		return f.Discovery.CommittedUpdates.Load() >= c
	}, retry.Delay(time.Millisecond))
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
	decode := kubelib.IstioCodec.UniversalDeserializer().Decode
	objectStrs := strings.Split(s, "---")
	for _, s := range objectStrs {
		if len(strings.TrimSpace(s)) == 0 {
			continue
		}
		o, _, err := decode([]byte(s), nil, nil)
		if err != nil {
			return nil, fmt.Errorf("failed deserializing kubernetes object: %v (%v)", err, s)
		}
		objects = append(objects, o)
	}
	return objects, nil
}

// DisableAuthorizationForSecret makes the authorization check always pass. Should be used only for tests.
func DisableAuthorizationForSecret(fake *fake.Clientset) {
	fake.Fake.PrependReactor("create", "subjectaccessreviews", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, &authorizationv1.SubjectAccessReview{
			Status: authorizationv1.SubjectAccessReviewStatus{
				Allowed: true,
			},
		}, nil
	})
}
