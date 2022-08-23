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
	"net/http"
	"strconv"
	"sync"
	"time"

	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"go.uber.org/atomic"
	"golang.org/x/time/rate"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/autoregistration"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/apigen"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/networking/core/v1alpha3/envoyfilter"
	"istio.io/istio/pilot/pkg/networking/grpcgen"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
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

	// concurrentPushLimit is a semaphore that limits the amount of concurrent XDS pushes.
	concurrentPushLimit chan struct{}
	// requestRateLimit limits the number of new XDS requests allowed. This helps prevent thundering hurd of incoming requests.
	requestRateLimit *rate.Limiter

	// InboundUpdates describes the number of configuration updates the discovery server has received
	InboundUpdates *atomic.Int64
	// CommittedUpdates describes the number of configuration updates the discovery server has
	// received, process, and stored in the push context. If this number is less than InboundUpdates,
	// there are updates we have not yet processed.
	// Note: This does not mean that all proxies have received these configurations; it is strictly
	// the push context, which means that the next push to a proxy will receive this configuration.
	CommittedUpdates *atomic.Int64

	// pushChannel is the buffer used for debouncing.
	// after debouncing the pushRequest will be sent to pushQueue
	pushChannel chan *model.PushRequest

	// mutex used for protecting Environment.PushContext
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
	WorkloadEntryController *autoregistration.Controller

	// serverReady indicates caches have been synced up and server is ready to process requests.
	serverReady atomic.Bool

	debounceOptions debounceOptions

	instanceID string

	// Cache for XDS resources
	Cache model.XdsCache

	// JwtKeyResolver holds a reference to the JWT key resolver instance.
	JwtKeyResolver *model.JwksResolver

	// ListRemoteClusters collects debug information about other clusters this istiod reads from.
	ListRemoteClusters func() []cluster.DebugInfo

	// ClusterAliases are aliase names for cluster. When a proxy connects with a cluster ID
	// and if it has a different alias we should use that a cluster ID for proxy.
	ClusterAliases map[cluster.ID]cluster.ID
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(env *model.Environment, instanceID string, clusterAliases map[string]string) *DiscoveryServer {
	out := &DiscoveryServer{
		Env:                 env,
		Generators:          map[string]model.XdsResourceGenerator{},
		ProxyNeedsPush:      DefaultProxyNeedsPush,
		concurrentPushLimit: make(chan struct{}, features.PushThrottle),
		requestRateLimit:    rate.NewLimiter(rate.Limit(features.RequestLimit), 1),
		InboundUpdates:      atomic.NewInt64(0),
		CommittedUpdates:    atomic.NewInt64(0),
		pushChannel:         make(chan *model.PushRequest, 10),
		pushQueue:           NewPushQueue(),
		debugHandlers:       map[string]string{},
		adsClients:          map[string]*Connection{},
		debounceOptions: debounceOptions{
			debounceAfter:     features.DebounceAfter,
			debounceMax:       features.DebounceMax,
			enableEDSDebounce: features.EnableEDSDebounce,
		},
		Cache:      model.DisabledCache{},
		instanceID: instanceID,
	}

	out.ClusterAliases = make(map[cluster.ID]cluster.ID)
	for alias := range clusterAliases {
		out.ClusterAliases[cluster.ID(alias)] = cluster.ID(clusterAliases[alias])
	}

	out.initJwksResolver()

	if features.EnableXDSCaching {
		out.Cache = model.NewXdsCache()
		// clear the cache as endpoint shards are modified to avoid cache write race
		out.Env.EndpointIndex.SetCache(out.Cache)
	}

	out.ConfigGenerator = core.NewConfigGenerator(out.Cache)

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
	log.Infof("All caches have been synced up in %v, marking server ready", time.Since(processStartTime))
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
		if registry.Provider() != provider.Kubernetes && registry.Provider() != provider.External {
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
					log.Infof("Push Status: %s", string(out))
				}
			}
			model.LastPushMutex.Unlock()
		case <-stopCh:
			return
		}
	}
}

// dropCacheForRequest clears the cache in response to a push request
func (s *DiscoveryServer) dropCacheForRequest(req *model.PushRequest) {
	// If we don't know what updated, cannot safely cache. Clear the whole cache
	if len(req.ConfigsUpdated) == 0 {
		s.Cache.ClearAll()
	} else {
		// Otherwise, just clear the updated configs
		s.Cache.Clear(req.ConfigsUpdated)
	}
}

// Push is called to push changes on config updates using ADS.
func (s *DiscoveryServer) Push(req *model.PushRequest) {
	if !req.Full {
		req.Push = s.globalPushContext()
		s.dropCacheForRequest(req)
		s.AdsPushAll(versionInfo(), req)
		return
	}
	// Reset the status during the push.
	oldPushContext := s.globalPushContext()
	if oldPushContext != nil {
		oldPushContext.OnConfigChange()
		// Push the previous push Envoy metrics.
		envoyfilter.RecordMetrics()
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
	log.Debugf("InitContext %v for push took %s", versionLocal, initContextTime)
	pushContextInitTime.Record(initContextTime.Seconds())

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

// Returns the global push context. This should be used with caution; generally the proxy-specific
// PushContext should be used to get the current state in the context of a single proxy. This should
// only be used for "global" lookups, such as initiating a new push to all proxies.
func (s *DiscoveryServer) globalPushContext() *model.PushContext {
	s.updateMutex.RLock()
	defer s.updateMutex.RUnlock()
	return s.Env.PushContext
}

// ConfigUpdate implements ConfigUpdater interface, used to request pushes.
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

	push := func(req *model.PushRequest, debouncedEvents int, startDebounce time.Time) {
		pushFn(req)
		updateSent.Add(int64(debouncedEvents))
		debounceTime.Record(time.Since(startDebounce).Seconds())
		freeCh <- struct{}{}
	}

	pushWorker := func() {
		eventDelay := time.Since(startDebounce)
		quietTime := time.Since(lastConfigUpdateTime)
		// it has been too long or quiet enough
		if eventDelay >= opts.debounceMax || quietTime >= opts.debounceAfter {
			if req != nil {
				pushCounter++
				if req.ConfigsUpdated == nil {
					log.Infof("Push debounce stable[%d] %d for reason %s: %v since last change, %v since last push, full=%v",
						pushCounter, debouncedEvents, reasonsUpdated(req),
						quietTime, eventDelay, req.Full)
				} else {
					log.Infof("Push debounce stable[%d] %d for config %s: %v since last change, %v since last push, full=%v",
						pushCounter, debouncedEvents, configsUpdated(req),
						quietTime, eventDelay, req.Full)
				}
				free = false
				go push(req, debouncedEvents, startDebounce)
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
				go func(req *model.PushRequest) {
					pushFn(req)
					updateSent.Inc()
				}(r)
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

func configsUpdated(req *model.PushRequest) string {
	configs := ""
	for key := range req.ConfigsUpdated {
		configs += key.String()
		break
	}
	if len(req.ConfigsUpdated) > 1 {
		more := fmt.Sprintf(" and %d more configs", len(req.ConfigsUpdated)-1)
		configs += more
	}
	return configs
}

func reasonsUpdated(req *model.PushRequest) string {
	switch len(req.Reason) {
	case 0:
		return "unknown"
	case 1:
		return string(req.Reason[0])
	default:
		return fmt.Sprintf("%s and %d more reasons", req.Reason[0], len(req.Reason)-1)
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
			var closed <-chan struct{}
			if client.stream != nil {
				closed = client.stream.Context().Done()
			} else {
				closed = client.deltaStream.Context().Done()
			}
			go func() {
				pushEv := &Event{
					pushRequest: push,
					done:        doneFunc,
				}

				select {
				case client.pushChannel <- pushEv:
					return
				case <-closed: // grpc stream was closed
					doneFunc()
					log.Infof("Client closed connection %v", client.conID)
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
		log.Errorf("XDS: failed to init push context: %v", err)
		// We can't push if we can't read the data - stick with previous version.
		pushContextErrors.Increment()
		return nil, err
	}

	if err := s.UpdateServiceShards(push); err != nil {
		return nil, err
	}

	s.updateMutex.Lock()
	s.Env.PushContext = push
	// Ensure we drop the cache in the lock to avoid races, where we drop the cache, fill it back up, then update push context
	s.dropCacheForRequest(req)
	s.updateMutex.Unlock()

	return push, nil
}

func (s *DiscoveryServer) sendPushes(stopCh <-chan struct{}) {
	doSendPushes(stopCh, s.concurrentPushLimit, s.pushQueue)
}

// InitGenerators initializes generators to be used by XdsServer.
func (s *DiscoveryServer) InitGenerators(env *model.Environment, systemNameSpace string, internalDebugMux *http.ServeMux) {
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

	s.Generators["api"] = apigen.NewGenerator(env.ConfigStore)
	s.Generators["api/"+v3.EndpointType] = edsGen

	s.Generators["api/"+TypeURLConnect] = s.StatusGen

	s.Generators["event"] = s.StatusGen
	s.Generators[TypeDebug] = NewDebugGen(s, systemNameSpace, internalDebugMux)
	s.Generators[v3.BootstrapType] = &BootstrapGenerator{Server: s}
}

// Shutdown shuts down DiscoveryServer components.
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
				log.Errorf("Failed to send internal event %s: %v", con.conID, err)
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

func (s *DiscoveryServer) WaitForRequestLimit(ctx context.Context) error {
	if s.requestRateLimit.Limit() == 0 {
		// Allow opt out when rate limiting is set to 0qps
		return nil
	}
	// Give a bit of time for queue to clear out, but if not fail fast. Client will connect to another
	// instance in best case, or retry with backoff.
	wait, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return s.requestRateLimit.Wait(wait)
}
