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

	discoveryv2 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"github.com/google/uuid"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/security/pkg/server/ca/authenticate"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/util/sets"
)

var (
	versionMutex sync.RWMutex
	// version is the timestamp of the last registry event.
	version = "0"
	// versionNum counts versions
	versionNum = atomic.NewUint64(0)

	periodicRefreshMetrics = 10 * time.Second

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
)

func init() {
	debounceAfter = features.DebounceAfter
	debounceMax = features.DebounceMax
	enableEDSDebounce = features.EnableEDSDebounce.Get()
}

// DiscoveryServer is Pilot's gRPC implementation for Envoy's v2 xds APIs
type DiscoveryServer struct {
	// Env is the model environment.
	Env *model.Environment

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemRegistry *memory.ServiceDiscovery

	// MemRegistry is used for debug and load testing, allow adding services. Visible for testing.
	MemConfigController model.ConfigStoreCache

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator core.ConfigGenerator

	// Generators allow customizing the generated config, based on the client metadata.
	// Key is the generator type - will match the Generator metadata to set the per-connection
	// default generator, or the combination of Generator metadata and TypeUrl to select a
	// different generator for a type.
	// Normal istio clients use the default generator - will not be impacted by this.
	Generators map[string]model.XdsResourceGenerator

	concurrentPushLimit chan struct{}

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	DebugConfigs bool

	// mutex protecting global structs updated or read by ADS service, including ConfigsUpdated and
	// shards.
	mutex sync.RWMutex

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
	Authenticators []authenticate.Authenticator

	// InternalGen is notified of connect/disconnect/nack on all connections
	InternalGen *InternalGen

	// serverReady indicates caches have been synced up and server is ready to process requests.
	serverReady bool
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
func NewDiscoveryServer(env *model.Environment, plugins []string) *DiscoveryServer {
	out := &DiscoveryServer{
		Env:                     env,
		ConfigGenerator:         core.NewConfigGenerator(plugins),
		Generators:              map[string]model.XdsResourceGenerator{},
		EndpointShardsByService: map[string]map[string]*EndpointShards{},
		concurrentPushLimit:     make(chan struct{}, features.PushThrottle),
		pushChannel:             make(chan *model.PushRequest, 10),
		pushQueue:               NewPushQueue(),
		DebugConfigs:            features.DebugConfigs,
		debugHandlers:           map[string]string{},
		adsClients:              map[string]*Connection{},
		serverReady:             false,
	}

	if features.XDSAuth {
		// This is equivalent with the mTLS authentication for workload-to-workload.
		// The GRPC server is configured in bootstrap.initSecureDiscoveryService, using the root
		// certificate as 'ClientCAs'. To accept additional signers for client identities - add them
		// there, will be used for CA signing as well.
		out.Authenticators = append(out.Authenticators, &authenticate.ClientCertAuthenticator{})

		// TODO: we may want to support JWT/OIDC auth as well - using the same list of auth as
		// CA. Will require additional refactoring - probably best for 1.7.
	}

	// Flush cached discovery responses when detecting jwt public key change.
	model.GetJwtKeyResolver().PushFunc = func() {
		out.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.UnknownTrigger}})
	}

	return out
}

// Register adds the ADS and EDS handles to the grpc server
func (s *DiscoveryServer) Register(rpcs *grpc.Server) {
	// Register v2 and v3 servers
	discovery.RegisterAggregatedDiscoveryServiceServer(rpcs, s)
	discoveryv2.RegisterAggregatedDiscoveryServiceServer(rpcs, s.createV2Adapter())
}

// CachesSynced is called when caches have been synced so that server can accept connections.
func (s *DiscoveryServer) CachesSynced() {
	s.updateMutex.Lock()
	s.serverReady = true
	s.updateMutex.Unlock()
}

func (s *DiscoveryServer) IsServerReady() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.serverReady
}

func (s *DiscoveryServer) Start(stopCh <-chan struct{}) {
	adsLog.Infof("Starting ADS server")
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
			push.Mutex.Lock()

			model.LastPushMutex.Lock()
			if model.LastPushStatus != push {
				model.LastPushStatus = push
				push.UpdateMetrics()
				out, _ := model.LastPushStatus.StatusJSON()
				adsLog.Infof("Push Status: %s", string(out))
			}
			model.LastPushMutex.Unlock()

			push.Mutex.Unlock()
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
		go s.AdsPushAll(versionInfo(), req)
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

	push, err := s.initPushContext(req, oldPushContext)
	if err != nil {
		return
	}

	versionLocal := time.Now().Format(time.RFC3339) + "/" + strconv.FormatUint(versionNum.Load(), 10)
	versionNum.Inc()
	initContextTime := time.Since(t0)
	adsLog.Debugf("InitContext %v for push took %s", versionLocal, initContextTime)

	versionMutex.Lock()
	version = versionLocal
	versionMutex.Unlock()

	req.Push = push
	go s.AdsPushAll(versionLocal, req)
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
	s.pushChannel <- req
}

// Debouncing and push request happens in a separate thread, it uses locks
// and we want to avoid complications, ConfigUpdate may already hold other locks.
// handleUpdates processes events from pushChannel
// It ensures that at minimum minQuiet time has elapsed since the last event before processing it.
// It also ensures that at most maxDelay is elapsed between receiving an event and processing it.
func (s *DiscoveryServer) handleUpdates(stopCh <-chan struct{}) {
	debounce(s.pushChannel, stopCh, s.Push)
}

// The debounce helper function is implemented to enable mocking
func debounce(ch chan *model.PushRequest, stopCh <-chan struct{}, pushFn func(req *model.PushRequest)) {
	var timeChan <-chan time.Time
	var startDebounce time.Time
	var lastConfigUpdateTime time.Time

	pushCounter := 0
	debouncedEvents := 0

	// Keeps track of the push requests. If updates are debounce they will be merged.
	var req *model.PushRequest

	free := true
	freeCh := make(chan struct{}, 1)

	push := func(req *model.PushRequest) {
		pushFn(req)
		freeCh <- struct{}{}
	}

	pushWorker := func() {
		eventDelay := time.Since(startDebounce)
		quietTime := time.Since(lastConfigUpdateTime)
		// it has been too long or quiet enough
		if eventDelay >= debounceMax || quietTime >= debounceAfter {
			if req != nil {
				pushCounter++
				adsLog.Infof("Push debounce stable[%d] %d: %v since last change, %v since last push, full=%v",
					pushCounter, debouncedEvents,
					quietTime, eventDelay, req.Full)

				free = false
				go push(req)
				req = nil
				debouncedEvents = 0
			}
		} else {
			timeChan = time.After(debounceAfter - quietTime)
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
			if !enableEDSDebounce && !r.Full {
				// trigger push now, just for EDS
				go pushFn(r)
				continue
			}

			lastConfigUpdateTime = time.Now()
			if debouncedEvents == 0 {
				timeChan = time.After(debounceAfter)
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
			client, info := queue.Dequeue()
			recordPushTriggers(info.Reason...)
			// Signals that a push is done by reading from the semaphore, allowing another send on it.
			doneFunc := func() {
				queue.MarkDone(client)
				<-semaphore
			}

			proxiesQueueTime.Record(time.Since(info.Start).Seconds())

			go func() {
				pushEv := &Event{
					full:           info.Full,
					push:           info.Push,
					done:           doneFunc,
					start:          info.Start,
					configsUpdated: info.ConfigsUpdated,
					noncePrefix:    info.Push.Version,
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

// initPushContext creates a global push context and stores it on the environment.
func (s *DiscoveryServer) initPushContext(req *model.PushRequest, oldPushContext *model.PushContext) (*model.PushContext, error) {
	push := model.NewPushContext()
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
