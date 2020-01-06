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

package v2

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/atomic"
	"google.golang.org/grpc"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/core"
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

const (
	typePrefix = "type.googleapis.com/envoy.api.v2."

	// Constants used for XDS

	// ClusterType is used for cluster discovery. Typically first request received
	ClusterType = typePrefix + "Cluster"
	// EndpointType is used for EDS and ADS endpoint discovery. Typically second request.
	EndpointType = typePrefix + "ClusterLoadAssignment"
	// EndpointGroupType is used for EGDS and ADS endpoint group discovery. Typically after EDS
	// requests if EGDS was enabled.
	EndpointGroupType = typePrefix + "EndpointGroup"
	// ListenerType is sent after clusters and endpoints.
	ListenerType = typePrefix + "Listener"
	// RouteType is sent after listeners.
	RouteType = typePrefix + "RouteConfiguration"
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
	MemRegistry *MemServiceDiscovery

	// ConfigGenerator is responsible for generating data plane configuration using Istio networking
	// APIs and service registry info
	ConfigGenerator core.ConfigGenerator

	concurrentPushLimit chan struct{}

	// DebugConfigs controls saving snapshots of configs for /debug/adsz.
	// Defaults to false, can be enabled with PILOT_DEBUG_ADSZ_CONFIG=1
	DebugConfigs bool

	// mutex protecting global structs updated or read by ADS service, including EDSUpdates and
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
	Shards map[string]*EndpointGroups

	// ServiceAccounts has the concatenation of all service accounts seen so far in endpoints.
	// This is updated on push, based on shards. If the previous list is different than
	// current list, a full push will be forced, to trigger a secure naming update.
	// Due to the larger time, it is still possible that connection errors will occur while
	// CDS is updated.
	ServiceAccounts map[string]bool
}

// EndpointGroups slices endpoints within a shard into small groups. It makes pushing efficient by
// pushing only a small subset of endpoints within the shard. The related xDS resource is called EGDS.
type EndpointGroups struct {
	// The fixed name prefix of each group names within this group set. Generately, group set and service
	// has 1:1 mapping relationship
	NamePrefix string

	// The designed size of each endpoint group. The actual size may be slightly different from this.
	GroupSize uint32

	// The number of groups constructed.
	GroupCount uint32

	// A reference copy of all endpoints within groups. This is used to support old method.
	IstioEndpoints []*model.IstioEndpoint

	// A map stores the endpoint groups. The key is the name of each group.
	IstioEndpointGroups map[string][]*model.IstioEndpoint
}

func (g *EndpointGroups) getEndpoints(groupName string) []*model.IstioEndpoint {
	if g == nil {
		return nil
	}

	// no group name specified, return all group endpoints
	if groupName == "" {
		return g.IstioEndpoints
	}

	_, _, groupID := ExtractEndpointGroupKeys(groupName)

	if eps, f := g.IstioEndpointGroups[groupID]; f {
		return eps
	}

	return nil
}

// ExtractEndpointGroupKeys extracts the keys within the group name string
// the key can be the form of "[hostname]-[namespace]-[clusterID]-[groupID]"
func ExtractEndpointGroupKeys(groupName string) (string, string, string) {
	if groupName == "" {
		return "", "", ""
	}

	keys := strings.Split(groupName, "-")
	if len(keys) != 3 {
		return "", "", ""
	}

	return keys[0], keys[1], keys[2]
}

// NewDiscoveryServer creates DiscoveryServer that sources data from Pilot's internal mesh data structures
func NewDiscoveryServer(env *model.Environment, plugins []string) *DiscoveryServer {
	out := &DiscoveryServer{
		Env:                     env,
		ConfigGenerator:         core.NewConfigGenerator(plugins),
		EndpointShardsByService: map[string]map[string]*EndpointShards{},
		concurrentPushLimit:     make(chan struct{}, features.PushThrottle),
		pushChannel:             make(chan *model.PushRequest, 10),
		pushQueue:               NewPushQueue(),
		DebugConfigs:            features.DebugConfigs,
		debugHandlers:           map[string]string{},
	}

	// Flush cached discovery responses when detecting jwt public key change.
	model.JwtKeyResolver.PushFunc = func() {
		out.ConfigUpdate(&model.PushRequest{Full: true, Reason: []model.TriggerReason{model.UnknownTrigger}})
	}

	return out
}

// Register adds the ADS and EDS handles to the grpc server
func (s *DiscoveryServer) Register(rpcs *grpc.Server) {
	ads.RegisterAggregatedDiscoveryServiceServer(rpcs, s)
}

// Start starts the discovery server instance
func (s *DiscoveryServer) Start(stopCh <-chan struct{}) {
	adsLog.Infof("Starting ADS server")
	go s.handleUpdates(stopCh)
	go s.periodicRefreshMetrics(stopCh)
	go s.sendPushes(stopCh)
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
	push := model.NewPushContext()
	if err := push.InitContext(s.Env, oldPushContext, req); err != nil {
		adsLog.Errorf("XDS: Failed to update services: %v", err)
		// We can't push if we can't read the data - stick with previous version.
		pushContextErrors.Increment()
		return
	}

	if err := s.updateServiceShards(push); err != nil {
		return
	}

	s.updateMutex.Lock()
	s.Env.PushContext = push
	s.updateMutex.Unlock()

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
				edsUpdates := info.EdsUpdates
				egdsUpdates := info.EgdsUpdates
				if info.Full {
					// Setting this to nil will trigger a full push
					edsUpdates = nil
					egdsUpdates = nil
				}

				select {
				case client.pushChannel <- &XdsEvent{
					push:               info.Push,
					edsUpdatedServices: edsUpdates,
					egdsUpdatedGroups:  egdsUpdates,
					done:               doneFunc,
					start:              info.Start,
					namespacesUpdated:  info.NamespacesUpdated,
					configTypesUpdated: info.ConfigTypesUpdated,
					noncePrefix:        info.Push.Version,
				}:
					return
				case <-client.stream.Context().Done(): // grpc stream was closed
					doneFunc()
					adsLog.Infof("Client closed connection %v", client.ConID)
				}
			}()
		}
	}
}

func (s *DiscoveryServer) sendPushes(stopCh <-chan struct{}) {
	doSendPushes(stopCh, s.concurrentPushLimit, s.pushQueue)
}

func (g *EndpointGroups) accept(newEps []*model.IstioEndpoint) map[string]struct{} {
	prevEps := g.IstioEndpoints
	g.IstioEndpoints = newEps

	// Means the EGDS feature has been disabled globally
	if g.GroupSize <= 0 {
		return nil
	}

	if len(newEps) > len(prevEps)*2 || len(newEps) < len(prevEps)/2 {
		return g.reshard()
	}

	// Calculate the diff in memory

	added := make([]*model.IstioEndpoint, 0, len(newEps))

MainAddedLoop:
	for _, nep := range newEps {
		for _, pep := range prevEps {
			if cmp.Equal(nep, pep) == true {
				continue MainAddedLoop
			}
		}

		added = append(added, nep)
	}

	removed := make([]*model.IstioEndpoint, 0, len(prevEps))

MainRemovedLoop:
	for _, pep := range prevEps {
		for _, nep := range newEps {
			if cmp.Equal(pep, nep) == true {
				continue MainRemovedLoop
			}
		}

		// The endpoint was not found in new list. Mark it removed.
		removed = append(removed, pep)
	}

	names := g.updateEndpointGroups(added, removed)

	return names
}

// updateEndpointGroups accepts changed groups with mapped endpoints. It does the update
// by replacing existing groups entirely.
func (g *EndpointGroups) updateEndpointGroups(updated []*model.IstioEndpoint, removed []*model.IstioEndpoint) map[string]struct{} {
	updatedGroupKeys := make(map[string]struct{})

	// Merge keys of changed groups.
	for _, ep := range updated {
		updatedGroupKeys[g.makeGroupKey(ep)] = struct{}{}
	}

	for _, ep := range removed {
		updatedGroupKeys[g.makeGroupKey(ep)] = struct{}{}
	}

	// Now reconstruct the group of changed.
	updatedGroups := make(map[string][]*model.IstioEndpoint, g.GroupCount)
	for _, ep := range g.IstioEndpoints {
		key := g.makeGroupKey(ep)

		if _, f := updatedGroupKeys[key]; !f {
			_, f := updatedGroups[key]
			if !f {
				updatedGroups[key] = make([]*model.IstioEndpoint, 0, g.GroupSize)
			}

			updatedGroups[key] = append(updatedGroups[key], ep)
		}
	}

	// Changed EGDS resource names
	names := make(map[string]struct{})

	for key, eps := range updatedGroups {
		g.IstioEndpointGroups[key] = eps
		names[key] = struct{}{}
	}

	return names
}

func (g *EndpointGroups) makeGroupKey(ep *model.IstioEndpoint) string {
	index := ep.HashUint32() % g.GroupCount
	key := fmt.Sprintf("%s-%d", g.NamePrefix, index)

	return key
}

func (g *EndpointGroups) reshard() map[string]struct{} {
	eps := g.IstioEndpoints

	// Reset the group map first
	g.IstioEndpointGroups = make(map[string][]*model.IstioEndpoint)

	// Total number of slices
	g.GroupCount = uint32(math.Ceil(float64(len(eps)) / float64(g.GroupSize)))

	for _, ep := range eps {
		key := g.makeGroupKey(ep)

		if group, f := g.IstioEndpointGroups[key]; !f {
			g.IstioEndpointGroups[key] = make([]*model.IstioEndpoint, 0, g.GroupSize)
		} else {
			group = append(group, ep)
		}
	}

	return nil
}
