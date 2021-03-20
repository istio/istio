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
	"strconv"
	"sync"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/controller/workloadentry"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/util/sets"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/security"
)

var (
	versionMutex sync.RWMutex
	// version is the timestamp of the last registry event.
	version = "0"
	// versionNum counts versions
	versionNum = atomic.NewUint64(0)

	periodicRefreshMetrics = 10 * time.Second
)

type debounceOptions struct {
	// debounceAfter is the delay added to events to wait
	// after a registry/config event for debouncing.
	// This will delay the push by at least this interval, plus
	// the time getting subsequent events. If no change is
	// detected the push will happen, otherwise we'll keep
	// delaying until things settle.
	debounceAfter time.Duration

	// debounceMax is the maximum time to wait for events
	// while debouncing. Defaults to 10 seconds. If events keep
	// showing up with no break for this time, we'll trigger a push.
	debounceMax time.Duration

	// enableEDSDebounce indicates whether EDS pushes should be debounced.
	enableEDSDebounce bool
}

// DiscoveryServer is Pilot's gRPC implementation for Envoy's xds APIs
type DiscoveryServer struct {
	// Env is the model environment.
	Env *model.Environment

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemRegistry *memory.ServiceDiscovery

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator core.ConfigGenerator

	// Generators allow customizing the generated config, based on the client metadata.
	// Key is the generator type - will match the Generator metadata to set the per-connection
	// default generator, or the combination of Generator metadata and TypeUrl to select a
	// different generator for a type.
	// Normal istio clients use the default generator - will not be impacted by this.
	Generators map[string]model.XdsResourceGenerator

	// ProxyNeedsPush is a function that determines whether a push can be completely skipped. Individual generators
	// may also choose to not send any updates.
	ProxyNeedsPush func(proxy *model.Proxy, req *model.PushRequest) bool

	concurrentPushLimit chan struct{}
	// mutex protecting global structs updated or read by ADS service, including ConfigsUpdated and
	// shards.
	mutex sync.RWMutex

	// InboundUpdates describes the number of configuration updates the discovery server has received
	InboundUpdates *atomic.Int64
	// CommittedUpdates describes the number of configuration updates the discovery server has
	// received, process, and stored in the push context. If this number is less than InboundUpdates,
	// there are updates we have not yet processed.
	// Note: This does not mean that all proxies have received these configurations; it is strictly
	// the push context, which means that the next push to a proxy will receive this configuration.
	CommittedUpdates *atomic.Int64

	// EndpointShards for a service. This is a global (per-server) list, built from
	// incremental updates. This is keyed by service and namespace
	EndpointShardsByService map[string]map[string]*EndpointShards

	pushChannel chan *model.PushRequest

	// mutex used for config update scheduling (former cache update mutex)
	updateMutex sync.RWMutex

	// pushQueue is the buffer that used after debounce and before the real xds push.
	pushQueue *PushQueue

	// debugHandlers is the list of all the supported debug handlers.
	debugHandlers map[string]string

	// adsClients reflect active gRPC channels, for both ADS and EDS.
	adsClients      map[string]*Connection
	adsClientsMutex sync.RWMutex

	StatusReporter DistributionStatusCache

	// Authenticators for XDS requests. Should be same/subset of the CA authenticators.
	Authenticators []security.Authenticator

	// StatusGen is notified of connect/disconnect/nack on all connections
	StatusGen               *StatusGen
	WorkloadEntryController *workloadentry.Controller

	// serverReady indicates caches have been synced up and server is ready to process requests.
	serverReady atomic.Bool

	debounceOptions debounceOptions

	instanceID string

	// Cache for XDS resources
	Cache model.XdsCache

	// JwtKeyResolver holds a reference to the JWT key resolver instance.
	JwtKeyResolver *model.JwksResolver
}

// EndpointShards holds the set of endpoint shards of a service. Registries update
// individual shards incrementally. The shards are aggregated and split into
// clusters when a push for the specific cluster is needed.
type EndpointShards struct {
	// mutex protecting below map.
	mutex sync.RWMutex

	// Shards is used to track the shards. EDS updates are grouped by shard.
	// Current implementation uses the registry name as key - in multicluster this is the
	// name of the k8s cluster, derived from the config (secret).
	Shards map[string][]*model.IstioEndpoint

	// ServiceAccounts has the concatenation of all service accounts seen so far in endpoints.
	// This is updated on push, based on shards. If the previous list is different than
	// current list, a full push will be forced, to trigger a secure naming update.
	// Due to the larger time, it is still possible that connection errors will occur while
	// CDS is updated.
	ServiceAccounts sets.Set
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(env *model.Environment, plugins []string, instanceID string, systemNameSpace string) *DiscoveryServer {
	out := &DiscoveryServer{
		Env:                     env,
		Generators:              map[string]model.XdsResourceGenerator{},
		ProxyNeedsPush:          DefaultProxyNeedsPush,
		EndpointShardsByService: map[string]map[string]*EndpointShards{},
		concurrentPushLimit:     make(chan struct{}, features.PushThrottle),
		InboundUpdates:          atomic.NewInt64(0),
		CommittedUpdates:        atomic.NewInt64(0),
		pushChannel:             make(chan *model.PushRequest, 10),
		pushQueue:               NewPushQueue(),
		debugHandlers:           map[string]string{},
		adsClients:              map[string]*Connection{},
		debounceOptions: debounceOptions{
			debounceAfter:     features.DebounceAfter,
			debounceMax:       features.DebounceMax,
			enableEDSDebounce: features.EnableEDSDebounce.Get(),
		},
		Cache:      model.DisabledCache{},
		instanceID: instanceID,
	}

	out.initJwksResolver()

	out.initGenerators(env, systemNameSpace)

	if features.EnableXDSCaching {
		out.Cache = model.NewXdsCache()
	}

	out.ConfigGenerator = core.NewConfigGenerator(plugins, out.Cache)

	return out
}

// initJwkResolver initializes the JWT key resolver to be used.
func (s *DiscoveryServer) initJwksResolver() {
	if s.JwtKeyResolver != nil {
		s.closeJwksResolver()
	}
	s.JwtKeyResolver = model.NewJwksResolver(
		model.JwtPubKeyEvictionDuration, model.JwtPubKeyRefreshInterval,
		model.JwtPubKeyRefreshIntervalOnFailure, model.JwtPubKeyRetryInterval)

	// Flush cached discovery responses when detecting jwt public key change.
	s.JwtKeyResolver.PushFunc = func() {
		s.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.UnknownTrigger}})
	}
}

// closeJwksResolver shuts down the JWT key resolver used.
func (s *DiscoveryServer) closeJwksResolver() {
	if s.JwtKeyResolver != nil {
		s.JwtKeyResolver.Close()
	}
	s.JwtKeyResolver = nil
}

// Register adds the ADS handler to the grpc server
func (s *DiscoveryServer) Register(rpcs *grpc.Server) {
	// Register v3 server
	discovery.RegisterAggregatedDiscoveryServiceServer(rpcs, s)
}

var processStartTime = time.Now()

// CachesSynced is called when caches have been synced so that server can accept connections.
func (s *DiscoveryServer) CachesSynced() {
	adsLog.Infof("All caches have been synced up in %v, marking server ready", time.Since(processStartTime))
	s.serverReady.Store(true)
}

func (s *DiscoveryServer) IsServerReady() bool {
	return s.serverReady.Load()
}

func (s *DiscoveryServer) Start(stopCh <-chan struct{}) {
	go s.WorkloadEntryController.Run(stopCh)
	go s.handleUpdates(stopCh)
	go s.periodicRefreshMetrics(stopCh)
	go s.sendPushes(stopCh)
}

func (s *DiscoveryServer) getNonK8sRegistries() []serviceregistry.Instance {
	var registries []serviceregistry.Instance
	var nonK8sRegistries []serviceregistry.Instance

	if agg, ok := s.Env.ServiceDiscovery.(*aggregate.Controller); ok {
		registries = agg.GetRegistries()
	} else {
		registries = []serviceregistry.Instance{
			serviceregistry.Simple{
				ServiceDiscovery: s.Env.ServiceDiscovery,
			},
		}
	}

	for _, registry := range registries {
		if registry.Provider() != serviceregistry.Kubernetes && registry.Provider() != serviceregistry.External {
			nonK8sRegistries = append(nonK8sRegistries, registry)
		}
	}
	return nonK8sRegistries
}

// Push metrics are updated periodically (10s default)
func (s *DiscoveryServer) periodicRefreshMetrics(stopCh <-chan struct{}) {
	ticker := time.NewTicker(periodicRefreshMetrics)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			push := s.globalPushContext()
			model.LastPushMutex.Lock()
			if model.LastPushStatus != push {
				model.LastPushStatus = push
				push.UpdateMetrics()
				out, _ := model.LastPushStatus.StatusJSON()
				if string(out) != "{}" {
					adsLog.Infof("Push Status: %s", string(out))
				}
			}
			model.LastPushMutex.Unlock()
		case <-stopCh:
			return
		}
	}
}

// Push is called to push changes on config updates using ADS. This is set in DiscoveryService.Push,
// to avoid direct dependencies.
func (s *DiscoveryServer) Push(req *model.PushRequest) {
	if !req.Full {
		req.Push = s.globalPushContext()
		s.AdsPushAll(versionInfo(), req)
		return
	}
	// Reset the status during the push.
	oldPushContext := s.globalPushContext()
	if oldPushContext != nil {
		oldPushContext.OnConfigChange()
	}
	// PushContext is reset after a config change. Previous status is
	// saved.
	t0 := time.Now()

	versionLocal := time.Now().Format(time.RFC3339) + "/" + strconv.FormatUint(versionNum.Inc(), 10)
	push, err := s.initPushContext(req, oldPushContext, versionLocal)
	if err != nil {
		return
	}

	initContextTime := time.Since(t0)
	adsLog.Debugf("InitContext %v for push took %s", versionLocal, initContextTime)

	versionMutex.Lock()
	version = versionLocal
	versionMutex.Unlock()

	req.Push = push
	s.AdsPushAll(versionLocal, req)
}

func nonce(noncePrefix string) string {
	return noncePrefix + uuid.New().String()
}

func versionInfo() string {
	versionMutex.RLock()
	defer versionMutex.RUnlock()
	return version
}

// Returns the global push context.
func (s *DiscoveryServer) globalPushContext() *model.PushContext {
	s.updateMutex.RLock()
	defer s.updateMutex.RUnlock()
	return s.Env.PushContext
}

// ConfigUpdate implements ConfigUpdater interface, used to request pushes.
// It replaces the 'clear cache' from v1.
func (s *DiscoveryServer) ConfigUpdate(req *model.PushRequest) {
	inboundConfigUpdates.Increment()
	s.InboundUpdates.Inc()
	s.pushChannel <- req
}

// Debouncing and push request happens in a separate thread, it uses locks
// and we want to avoid complications, ConfigUpdate may already hold other locks.
// handleUpdates processes events from pushChannel
// It ensures that at minimum minQuiet time has elapsed since the last event before processing it.
// It also ensures that at most maxDelay is elapsed between receiving an event and processing it.
func (s *DiscoveryServer) handleUpdates(stopCh <-chan struct{}) {
	debounce(s.pushChannel, stopCh, s.debounceOptions, s.Push, s.CommittedUpdates)
}

// The debounce helper function is implemented to enable mocking
func debounce(ch chan *model.PushRequest, stopCh <-chan struct{}, opts debounceOptions, pushFn func(req *model.PushRequest), updateSent *atomic.Int64) {
	var timeChan <-chan time.Time
	var startDebounce time.Time
	var lastConfigUpdateTime time.Time

	pushCounter := 0
	debouncedEvents := 0

	// Keeps track of the push requests. If updates are debounce they will be merged.
	var req *model.PushRequest

	free := true
	freeCh := make(chan struct{}, 1)

	push := func(req *model.PushRequest, debouncedEvents int) {
		pushFn(req)
		updateSent.Add(int64(debouncedEvents))
		freeCh <- struct{}{}
	}

	pushWorker := func() {
		eventDelay := time.Since(startDebounce)
		quietTime := time.Since(lastConfigUpdateTime)
		// it has been too long or quiet enough
		if eventDelay >= opts.debounceMax || quietTime >= opts.debounceAfter {
			if req != nil {
				pushCounter++
				adsLog.Infof("Push debounce stable[%d] %d: %v since last change, %v since last push, full=%v",
					pushCounter, debouncedEvents,
					quietTime, eventDelay, req.Full)

				free = false
				go push(req, debouncedEvents)
				req = nil
				debouncedEvents = 0
			}
		} else {
			timeChan = time.After(opts.debounceAfter - quietTime)
		}
	}

	for {
		select {
		case <-freeCh:
			free = true
			pushWorker()
		case r := <-ch:
			// If reason is not set, record it as an unknown reason
			if len(r.Reason) == 0 {
				r.Reason = []model.TriggerReason{model.UnknownTrigger}
			}
			if !opts.enableEDSDebounce && !r.Full {
				// trigger push now, just for EDS
				go pushFn(r)
				continue
			}

			lastConfigUpdateTime = time.Now()
			if debouncedEvents == 0 {
				timeChan = time.After(opts.debounceAfter)
				startDebounce = lastConfigUpdateTime
			}
			debouncedEvents++

			req = req.Merge(r)
		case <-timeChan:
			if free {
				pushWorker()
			}
		case <-stopCh:
			return
		}
	}
}

func doSendPushes(stopCh <-chan struct{}, semaphore chan struct{}, queue *PushQueue) {
	for {
		select {
		case <-stopCh:
			return
		default:
			// We can send to it until it is full, then it will block until a pushes finishes and reads from it.
			// This limits the number of pushes that can happen concurrently
			semaphore <- struct{}{}

			// Get the next proxy to push. This will block if there are no updates required.
			client, push, shuttingdown := queue.Dequeue()
			if shuttingdown {
				return
			}
			recordPushTriggers(push.Reason...)
			// Signals that a push is done by reading from the semaphore, allowing another send on it.
			doneFunc := func() {
				queue.MarkDone(client)
				<-semaphore
			}

			proxiesQueueTime.Record(time.Since(push.Start).Seconds())

			go func() {
				pushEv := &Event{
					pushRequest: push,
					done:        doneFunc,
				}

				select {
				case client.pushChannel <- pushEv:
					return
				case <-client.stream.Context().Done(): // grpc stream was closed
					doneFunc()
					adsLog.Infof("Client closed connection %v", client.ConID)
				}
			}()
		}
	}
}

// initPushContext creates a global push context and stores it on the environment. Note: while this
// method is technically thread safe (there are no data races), it should not be called in parallel;
// if it is, then we may start two push context creations (say A, and B), but then write them in
// reverse order, leaving us with a final version of A, which may be incomplete.
func (s *DiscoveryServer) initPushContext(req *model.PushRequest, oldPushContext *model.PushContext, version string) (*model.PushContext, error) {
	push := model.NewPushContext()
	push.PushVersion = version
	push.JwtKeyResolver = s.JwtKeyResolver
	if err := push.InitContext(s.Env, oldPushContext, req); err != nil {
		adsLog.Errorf("XDS: Failed to update services: %v", err)
		// We can't push if we can't read the data - stick with previous version.
		pushContextErrors.Increment()
		return nil, err
	}

	if err := s.UpdateServiceShards(push); err != nil {
		return nil, err
	}

	s.updateMutex.Lock()
	s.Env.PushContext = push
	s.updateMutex.Unlock()

	return push, nil
}

func (s *DiscoveryServer) sendPushes(stopCh <-chan struct{}) {
	doSendPushes(stopCh, s.concurrentPushLimit, s.pushQueue)
}

// initGenerators initializes generators to be used by XdsServer.
func (s *DiscoveryServer) initGenerators(env *model.Environment, systemNameSpace string) {
	edsGen := &EdsGenerator{Server: s}
	s.StatusGen = NewStatusGen(s)
	s.Generators[v3.ClusterType] = &CdsGenerator{Server: s}
	s.Generators[v3.ListenerType] = &LdsGenerator{Server: s}
	s.Generators[v3.RouteType] = &RdsGenerator{Server: s}
	s.Generators[v3.EndpointType] = edsGen
	s.Generators[v3.NameTableType] = &NdsGenerator{Server: s}
	s.Generators[v3.ExtensionConfigurationType] = &EcdsGenerator{Server: s}
	s.Generators[v3.ProxyConfigType] = &PcdsGenerator{Server: s, TrustBundle: env.TrustBundle}

	s.Generators["grpc"] = &grpcgen.GrpcConfigGenerator{}
	s.Generators["grpc/"+v3.EndpointType] = edsGen
	s.Generators["grpc/"+v3.ListenerType] = s.Generators["grpc"]
	s.Generators["grpc/"+v3.RouteType] = s.Generators["grpc"]
	s.Generators["grpc/"+v3.ClusterType] = s.Generators["grpc"]

	s.Generators["api"] = &apigen.APIGenerator{}
	s.Generators["api/"+v3.EndpointType] = edsGen

	s.Generators["api/"+TypeURLConnect] = s.StatusGen

	s.Generators["event"] = s.StatusGen
	s.Generators[TypeDebug] = NewDebugGen(s, systemNameSpace)
}

// shutdown shuts down DiscoveryServer components.
func (s *DiscoveryServer) Shutdown() {
	s.closeJwksResolver()
	s.pushQueue.ShutDown()
}

// Clients returns all currently connected clients. This method can be safely called concurrently,
// but care should be taken with the underlying objects (ie model.Proxy) to ensure proper locking.
// This method returns only fully initialized connections; for all connections, use AllClients
func (s *DiscoveryServer) Clients() []*Connection {
	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()
	clients := make([]*Connection, 0, len(s.adsClients))
	for _, con := range s.adsClients {
		select {
		case <-con.initialized:
		default:
			// Initialization not complete, skip
			continue
		}
		clients = append(clients, con)
	}
	return clients
}

// AllClients returns all connected clients, per Clients, but additionally includes unintialized connections
// Warning: callers must take care not to rely on the con.proxy field being set
func (s *DiscoveryServer) AllClients() []*Connection {
	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()
	clients := make([]*Connection, 0, len(s.adsClients))
	for _, con := range s.adsClients {
		clients = append(clients, con)
	}
	return clients
}

// SendResponse will immediately send the response to all connections.
// TODO: additional filters can be added, for example namespace.
func (s *DiscoveryServer) SendResponse(connections []*Connection, res *discovery.DiscoveryResponse) {
	for _, p := range connections {
		// p.send() waits for an ACK - which is reasonable for normal push,
		// but in this case we want to sync fast and not bother with stuck connections.
		// This is expecting a relatively small number of watchers - each other istiod
		// plus few admin tools or bridges to real message brokers. The normal
		// push expects 1000s of envoy connections.
		con := p
		go func() {
			err := con.stream.Send(res)
			if err != nil {
				adsLog.Info("Failed to send internal event ", con.ConID, " ", err)
			}
		}()
	}
}

// nolint
// ClientsOf returns the clients that are watching the given resource.
func (s *DiscoveryServer) ClientsOf(typeUrl string) []*Connection {
	pending := []*Connection{}
	for _, v := range s.Clients() {
		if v.Watching(typeUrl) {
			pending = append(pending, v)
		}
	}

	return pending
}
