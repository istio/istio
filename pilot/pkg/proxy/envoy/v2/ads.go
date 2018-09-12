// Copyright 2017 Istio Authors
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
	"context"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v2alpha"
	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/gogo/protobuf/types"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/model"
	istiolog "istio.io/istio/pkg/log"
)

var (
	adsLog = istiolog.RegisterScope("ads", "ads debugging", 0)

	// adsClients reflect active gRPC channels, for both ADS and EDS.
	adsClients      = map[string]*XdsConnection{}
	adsClientsMutex sync.RWMutex

	// Map of sidecar IDs to XdsConnections, first key is sidecarID, second key is connID
	// This is a map due to an edge case during envoy restart whereby the 'old' envoy
	// reconnects after the 'new/restarted' envoy
	adsSidecarIDConnectionsMap = map[string]map[string]*XdsConnection{}

	// ackTimeout is the max time to wait for a ADS send with ack to complete. This helps detect
	// clients in a bad state (not reading).
	ackTimeout = 10 * time.Second

	// pushTimeout is the time to wait for a push on a client. Pilot iterates over
	// clients and pushes them serially for now, to avoid large CPU/memory spikes.
	// We measure and reports cases where pushing a client takes longer.
	pushTimeout = 10 * time.Second

	allowConcurrentPush = false
)

var (
	timeZero time.Time
)

var (
	// experiment on getting some monitoring on config errors.
	cdsReject = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_xds_cds_reject",
		Help: "Pilot rejected CSD configs.",
	}, []string{"node", "err"})

	edsReject = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_xds_eds_reject",
		Help: "Pilot rejected EDS.",
	}, []string{"node", "err"})

	edsInstances = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_xds_eds_instances",
		Help: "Instances for each cluster, as of last push. Zero instances is an error",
	}, []string{"cluster"})

	ldsReject = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_xds_lds_reject",
		Help: "Pilot rejected LDS.",
	}, []string{"node", "err"})

	rdsReject = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "pilot_xds_rds_reject",
		Help: "Pilot rejected RDS.",
	}, []string{"node", "err"})

	monServices = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_services",
		Help: "Total services known to pilot",
	})

	monVServices = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_virt_services",
		Help: "Total virtual services known to pilot",
	})

	xdsClients = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "pilot_xds",
		Help: "Number of endpoints connected to this pilot using XDS",
	})

	writeTimeout = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "pilot_xds_write_timeout",
		Help: "Pilot write timeout",
	})

	pushTimeouts = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "pilot_xds_push_timeout",
		Help: "Pilot push timeout",
	})

	pushes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pilot_xds_pushes",
		Help: "Pilot push timeout",
	}, []string{"type"})

	pushErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pilot_xds_push_errors",
		Help: "Number of errors (timeouts) pushing to sidecars.",
	}, []string{"type"})
)

func init() {
	prometheus.MustRegister(cdsReject)
	prometheus.MustRegister(edsReject)
	prometheus.MustRegister(ldsReject)
	prometheus.MustRegister(rdsReject)
	prometheus.MustRegister(edsInstances)
	prometheus.MustRegister(monServices)
	prometheus.MustRegister(monVServices)
	prometheus.MustRegister(xdsClients)
	prometheus.MustRegister(writeTimeout)
	prometheus.MustRegister(pushTimeouts)
	prometheus.MustRegister(pushes)
	prometheus.MustRegister(pushErrors)

	// Experimental env to disable push suppression
	// By default a pod will not receive a push from an older version if a
	// push for a newer version has started. This behavior can be disabled
	// for testing or in special cases (bugs or corner cases found, etc)
	// Setting the env variable to any non-empty value will result in 0.8
	// behaviour, default is to cancel old pushes when possible and lock.
	allowConcurrentPush = os.Getenv("PILOT_ALLOW_CONCURRENT") != ""
}

// DiscoveryStream is a common interface for EDS and ADS. It also has a
// shorter name.
type DiscoveryStream interface {
	Send(*xdsapi.DiscoveryResponse) error
	Recv() (*xdsapi.DiscoveryRequest, error)
	grpc.ServerStream
}

// XdsConnection is a listener connection type.
type XdsConnection struct {
	// Mutex to protect changes to this XDS connection
	mu sync.RWMutex

	// PeerAddr is the address of the client envoy, from network layer
	PeerAddr string

	// Time of connection, for debugging
	Connect time.Time

	// ConID is the connection identifier, used as a key in the connection table.
	// Currently based on the node name and a counter.
	ConID string

	modelNode *model.Proxy

	// Sending on this channel results in a push. We may also make it a channel of objects so
	// same info can be sent to all clients, without recomputing.
	pushChannel chan *XdsEvent

	// Set to the current push status when a push is started.
	// Null after the pushall completes for the node.
	currentPush *model.PushContext

	// Set if a push request is received while a push is in progress.
	// Will keep getting updated with the latest push version.
	nextPush *model.PushContext

	// doneChannel will be closed when the client is closed.
	doneChannel chan struct{}

	ack         sync.Mutex
	ackChannels map[string]chan struct{}

	// TODO: migrate other fields as needed from model.Proxy and replace it

	//HttpConnectionManagers map[string]*http_conn.HttpConnectionManager

	LDSListeners []*xdsapi.Listener                    `json:"-"`
	RouteConfigs map[string]*xdsapi.RouteConfiguration `json:"-"`
	CDSClusters  []*xdsapi.Cluster

	// Last nonce sent and ack'd (timestamps) used for debugging
	ClusterNonceSent, ClusterNonceAcked   string
	ListenerNonceSent, ListenerNonceAcked string
	RouteNonceSent, RouteNonceAcked       string
	EndpointNonceSent, EndpointNonceAcked string
	EndpointPercent                       int

	// current list of clusters monitored by the client
	Clusters []string

	// TODO: TcpListeners (may combine mongo/etc)

	// Both ADS and EDS streams implement this interface
	stream DiscoveryStream

	// Routes is the list of watched Routes.
	Routes []string

	// LDSWatch is set if the remote server is watching Listeners
	LDSWatch bool
	// CDSWatch is set if the remote server is watching Clusters
	CDSWatch bool

	// added will be true if at least one discovery request was received, and the connection
	// is added to the map of active.
	added bool

	// Time of last push
	LastPush time.Time

	// Time of last push failure.
	LastPushFailure time.Time

	// pushMutex prevents 2 overlapping pushes for this connection.
	pushMutex sync.Mutex
}

// configDump converts the connection internal state into an Envoy Admin API config dump proto
// It is used in debugging to create a consistent object for comparison between Envoy and Pilot outputs
func (s *DiscoveryServer) configDump(conn *XdsConnection) (*adminapi.ConfigDump, error) {
	dynamicActiveClusters := []adminapi.ClustersConfigDump_DynamicCluster{}
	clusters, err := s.generateRawClusters(conn.modelNode, s.env.PushContext)
	if err != nil {
		return nil, err
	}
	for _, cs := range clusters {
		dynamicActiveClusters = append(dynamicActiveClusters, adminapi.ClustersConfigDump_DynamicCluster{Cluster: cs})
	}
	clustersAny, err := types.MarshalAny(&adminapi.ClustersConfigDump{
		VersionInfo:           versionInfo(),
		DynamicActiveClusters: dynamicActiveClusters,
	})
	if err != nil {
		return nil, err
	}

	dynamicActiveListeners := []adminapi.ListenersConfigDump_DynamicListener{}
	listeners, err := s.generateRawListeners(conn, s.env.PushContext)
	if err != nil {
		return nil, err
	}
	for _, cs := range listeners {
		dynamicActiveListeners = append(dynamicActiveListeners, adminapi.ListenersConfigDump_DynamicListener{Listener: cs})
	}
	listenersAny, err := types.MarshalAny(&adminapi.ListenersConfigDump{
		VersionInfo:            versionInfo(),
		DynamicActiveListeners: dynamicActiveListeners,
	})
	if err != nil {
		return nil, err
	}

	routes, err := s.generateRawRoutes(conn, s.env.PushContext)
	if err != nil {
		return nil, err
	}
	routeConfigAny, _ := types.MarshalAny(&adminapi.RoutesConfigDump{})
	if len(routes) > 0 {
		dynamicRouteConfig := []adminapi.RoutesConfigDump_DynamicRouteConfig{}
		for _, rs := range routes {
			dynamicRouteConfig = append(dynamicRouteConfig, adminapi.RoutesConfigDump_DynamicRouteConfig{RouteConfig: rs})
		}
		routeConfigAny, err = types.MarshalAny(&adminapi.RoutesConfigDump{DynamicRouteConfigs: dynamicRouteConfig})
		if err != nil {
			return nil, err
		}
	}

	bootstrapAny, _ := types.MarshalAny(&adminapi.BootstrapConfigDump{})
	// The config dump must have all configs with order specified in
	// https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	configDump := &adminapi.ConfigDump{Configs: []types.Any{*bootstrapAny, *clustersAny, *listenersAny, *routeConfigAny}}
	return configDump, nil
}

// XdsEvent represents a config or registry event that results in a push.
type XdsEvent struct {

	// If not empty, it is used to indicate the event is caused by a change in the clusters.
	// Only EDS for the listed clusters will be sent.
	clusters []string

	push *model.PushContext

	pending *int32

	version string
}

func newXdsConnection(peerAddr string, stream DiscoveryStream) *XdsConnection {
	return &XdsConnection{
		pushChannel:  make(chan *XdsEvent, 1),
		doneChannel:  make(chan struct{}),
		ackChannels:  make(map[string]chan struct{}),
		PeerAddr:     peerAddr,
		Clusters:     []string{},
		Connect:      time.Now(),
		stream:       stream,
		LDSListeners: []*xdsapi.Listener{},
		RouteConfigs: map[string]*xdsapi.RouteConfiguration{},
	}
}

func receiveThread(con *XdsConnection, reqChannel chan *xdsapi.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || err == io.EOF {
				adsLog.Infof("ADS: %q %s terminated %v", con.PeerAddr, con.ConID, err)
				return
			}
			*errP = err
			adsLog.Errorf("ADS: %q %s terminated with errors %v", con.PeerAddr, con.ConID, err)
			return
		}
		if req.ResponseNonce != "" {
			con.notifyAck(req.ResponseNonce)
		}

		reqChannel <- req
	}
}

// StreamAggregatedResources implements the ADS interface.
func (s *DiscoveryServer) StreamAggregatedResources(stream ads.AggregatedDiscoveryService_StreamAggregatedResourcesServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "0.0.0.0"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}
	var discReq *xdsapi.DiscoveryRequest

	t0 := time.Now()
	// rate limit the herd, after restart all endpoints will reconnect to the
	// poor new pilot and overwhelm it.
	// TODO: instead of readiness probe, let endpoints connect and wait here for
	// config to become stable. Will better spread the load.
	s.rateLimiter.Wait(context.TODO())

	// first call - lazy loading, in tests. This should not happen if readiness
	// check works, since it assumes ClearCache is called (and as such PushContext
	// is initialized)
	// InitContext returns immediately if the context was already initialized.
	err := s.env.PushContext.InitContext(s.env)
	if err != nil {
		// Error accessing the data - log and close, maybe a different pilot replica
		// has more luck
		adsLog.Warnf("Error reading config %v", err)
		return err
	}
	if s.env.PushContext.Services == nil {
		// Error accessing the data - log and close, maybe a different pilot replica
		// has more luck
		adsLog.Warnf("Not initialized %v", s.env.PushContext)
		return err
	}

	con := newXdsConnection(peerAddr, stream)
	defer close(con.doneChannel)

	// Do not call: defer close(con.pushChannel) !
	// the push channel will be garbage collected when the connection is no longer used.
	// Closing the channel can cause subtle race conditions with push. According to the spec:
	// "It's only necessary to close a channel when it is important to tell the receiving goroutines that all data
	// have been sent."

	// Reading from a stream is a blocking operation. Each connection needs to read
	// discovery requests and wait for push commands on config change, so we add a
	// go routine. If go grpc adds gochannel support for streams this will not be needed.
	// This also detects close.
	var receiveError error
	reqChannel := make(chan *xdsapi.DiscoveryRequest, 1)
	go receiveThread(con, reqChannel, &receiveError)

	for {
		// Block until either a request is received or a push is triggered.
		select {
		case discReq, ok = <-reqChannel:
			if !ok {
				// Remote side closed connection.
				return receiveError
			}
			if discReq.Node == nil || discReq.Node.Id == "" {
				adsLog.Infof("Missing node id %s", discReq.String())
				continue
			}
			nt, err := model.ParseServiceNode(discReq.Node.Id)
			if err != nil {
				return err
			}
			nt.Metadata = model.ParseMetadata(discReq.Node.Metadata)
			con.mu.Lock()
			con.modelNode = &nt
			con.mu.Unlock()
			if con.ConID == "" {
				// first request
				con.ConID = connectionID(discReq.Node.Id)
			}

			switch discReq.TypeUrl {
			case ClusterType:
				if con.CDSWatch {
					// Already received a cluster watch request, this is an ACK
					if discReq.ErrorDetail != nil {
						adsLog.Warnf("ADS:CDS: ACK ERROR %v %s %v", peerAddr, con.ConID, discReq.String())
						cdsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
					} else if discReq.ResponseNonce != "" {
						con.ClusterNonceAcked = discReq.ResponseNonce
					}
					adsLog.Debugf("ADS:CDS: ACK %v %v", peerAddr, discReq.String())
					continue
				}
				// CDS REQ is the first request an envoy makes. This shows up
				// immediately after connect. It is followed by EDS REQ as
				// soon as the CDS push is returned.
				adsLog.Infof("ADS:CDS: REQ %v %s %v raw: %s", peerAddr, con.ConID, time.Since(t0), discReq.String())
				con.CDSWatch = true
				err := s.pushCds(con, s.env.PushContext)
				if err != nil {
					return err
				}

			case ListenerType:
				if con.LDSWatch {
					// Already received a cluster watch request, this is an ACK
					if discReq.ErrorDetail != nil {
						adsLog.Warnf("ADS:LDS: ACK ERROR %v %s %v", peerAddr, con.modelNode.ID, discReq.String())
						ldsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
					} else if discReq.ResponseNonce != "" {
						con.ListenerNonceAcked = discReq.ResponseNonce
					}
					adsLog.Debugf("ADS:LDS: ACK %v", discReq.String())
					continue
				}
				// too verbose - sent immediately after EDS response is received
				adsLog.Debugf("ADS:LDS: REQ %s %v", con.ConID, peerAddr)
				con.LDSWatch = true
				err := s.pushLds(con, s.env.PushContext, versionInfo())
				if err != nil {
					return err
				}

			case RouteType:
				routes := discReq.GetResourceNames()

				if len(routes) == len(con.Routes) || len(routes) == 0 {
					// routes and con.Routes are all empty, this is not an ack and will do nothing.
					if len(routes) == 0 && len(con.Routes) == 0 {
						continue
					}

					sort.Strings(routes)
					sort.Strings(con.Routes)

					if reflect.DeepEqual(con.Routes, routes) || len(routes) == 0 {
						if discReq.ErrorDetail != nil {
							adsLog.Warnf("ADS:RDS: ACK ERROR %v %s (%s) %v", peerAddr, con.ConID, con.modelNode, discReq.String())
							rdsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
						}
						// Not logging full request, can be very long.
						adsLog.Debugf("ADS:RDS: ACK %s %s (%s) %s %s", peerAddr, con.ConID, con.modelNode, discReq.VersionInfo, discReq.ResponseNonce)
						if len(con.Routes) > 0 {
							// Already got a list of routes to watch and has same length as the request, this is an ack
							if discReq.ErrorDetail == nil && discReq.ResponseNonce != "" {
								con.mu.Lock()
								con.RouteNonceAcked = discReq.ResponseNonce
								con.mu.Unlock()
							}
							continue
						}
					}
				}

				con.Routes = routes
				adsLog.Debugf("ADS:RDS: REQ %s %s  routes: %d", peerAddr, con.ConID, len(con.Routes))
				err := s.pushRoute(con, s.env.PushContext)
				if err != nil {
					return err
				}

			case EndpointType:
				clusters := discReq.GetResourceNames()
				if discReq.ErrorDetail != nil {
					adsLog.Warnf("ADS:EDS: ACK ERROR %v %s %v", peerAddr, con.ConID, discReq.String())
					edsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
				}

				if len(clusters) == len(con.Clusters) {
					// clusters and con.Clusters are all empty, this is not an ack and will do nothing.
					if len(clusters) == 0 && len(con.Clusters) == 0 {
						continue
					}

					sort.Strings(clusters)
					sort.Strings(con.Clusters)

					// Already got a list of endpoints to watch and it is the same as the request, this is an ack
					if reflect.DeepEqual(con.Clusters, clusters) {
						if discReq.ErrorDetail == nil && discReq.ResponseNonce != "" {
							con.EndpointNonceAcked = discReq.ResponseNonce
							if len(edsClusters) != 0 {
								con.EndpointPercent = int((float64(len(clusters)) / float64(len(edsClusters))) * float64(100))
							}
						}
						continue
					}
				}

				for _, cn := range con.Clusters {
					s.removeEdsCon(cn, con.ConID, con)
				}

				for _, cn := range clusters {
					s.addEdsCon(cn, con.ConID, con)
				}

				con.Clusters = clusters
				adsLog.Debugf("ADS:EDS: REQ %s %s clusters: %d", peerAddr, con.ConID, len(con.Clusters))
				err := s.pushEds(s.env.PushContext, con)
				if err != nil {
					return err
				}

			default:
				adsLog.Warnf("ADS: Unknown watched resources %s", discReq.String())
			}

			if !con.added {
				con.added = true
				s.addCon(con.ConID, con)
				defer s.removeCon(con.ConID, con)
			}
		case pushEv, _ := <-con.pushChannel:
			// It is called when config changes.
			// This is not optimized yet - we should detect what changed based on event and only
			// push resources that need to be pushed.

			err := s.pushAll(con, pushEv)
			if err != nil {
				return nil
			}
		}
	}
}

// IncrementalAggregatedResources is not implemented.
func (s *DiscoveryServer) IncrementalAggregatedResources(stream ads.AggregatedDiscoveryService_IncrementalAggregatedResourcesServer) error {
	return status.Errorf(codes.Unimplemented, "not implemented")
}

// Compute and send the new configuration. This is blocking and may be slow
// for large configs.
func (s *DiscoveryServer) pushAll(con *XdsConnection, pushEv *XdsEvent) error {
	s.rateLimiter.Wait(context.TODO()) // rate limit the actual push

	// Prevent 2 overlapping pushes. Disabled if push suppression is disabled
	// (as fail-safe in case of bugs)
	if !allowConcurrentPush {
		con.pushMutex.Lock()
		defer con.pushMutex.Unlock()
	}

	defer func() {
		n := atomic.AddInt32(pushEv.pending, -1)
		if n <= 0 && pushEv.push.End == timeZero {
			// Display again the push status
			pushEv.push.End = time.Now()
			out, _ := pushEv.push.JSON()
			adsLog.Infof("Push finished: %v %s",
				time.Since(pushEv.push.Start), string(out))
		}
	}()
	// check version, suppress if changed.
	currentVersion := versionInfo()
	if !allowConcurrentPush && pushEv.version != currentVersion {
		adsLog.Infof("Suppress push for %s at %s, push with newer version %s in progress", con.ConID, pushEv.version, currentVersion)
		return nil
	}

	if con.CDSWatch {
		err := s.pushCds(con, pushEv.push)
		if err != nil {
			return err
		}
	}
	if len(con.Routes) > 0 {
		err := s.pushRoute(con, pushEv.push)
		if err != nil {
			return err
		}
	}
	if len(con.Clusters) > 0 {
		err := s.pushEds(pushEv.push, con)
		if err != nil {
			return err
		}
	}
	if con.LDSWatch {
		err := s.pushLds(con, pushEv.push, pushEv.version)
		if err != nil {
			return err
		}
	}
	return nil
}

func adsClientCount() int {
	var n int
	adsClientsMutex.RLock()
	n = len(adsClients)
	adsClientsMutex.RUnlock()
	return n
}

// AdsPushAll is used only by tests (after refactoring)
func AdsPushAll(s *DiscoveryServer) {
	s.AdsPushAll(versionInfo())
}

// AdsPushAll implements old style invalidation, generated when any rule or endpoint changes.
// Primary code path is from v1 discoveryService.clearCache(), which is added as a handler
// to the model ConfigStorageCache and Controller.
func (s *DiscoveryServer) AdsPushAll(version string) {
	pushContext := s.env.PushContext

	adsLog.Infof("XDS: Pushing %s Services: %d, "+
		"VirtualServices: %d, ConnectedEndpoints: %d", version,
		len(pushContext.Services), len(pushContext.VirtualServiceConfigs), adsClientCount())
	monServices.Set(float64(len(pushContext.Services)))
	monVServices.Set(float64(len(pushContext.VirtualServiceConfigs)))

	t0 := time.Now()

	// First update all cluster load assignments. This is computed for each cluster once per config change
	// instead of once per endpoint.
	edsClusterMutex.Lock()
	// Create a temp map to avoid locking the add/remove
	cMap := make(map[string]*EdsCluster, len(edsClusters))
	for k, v := range edsClusters {
		cMap[k] = v
	}
	edsClusterMutex.Unlock()

	// UpdateCluster updates the cluster with a mutex, this code is safe ( but computing
	// the update may be duplicated if multiple goroutines compute at the same time).
	// In general this code is called from the 'event' callback that is throttled.
	for clusterName, edsCluster := range cMap {
		if err := s.updateCluster(pushContext, clusterName, edsCluster); err != nil {
			adsLog.Errorf("updateCluster failed with clusterName %s", clusterName)
		}
	}
	adsLog.Infof("Cluster init time %v %s", time.Since(t0), version)

	// Push config changes, iterating over connected envoys. This cover ADS and EDS(0.7), both share
	// the same connection table
	adsClientsMutex.RLock()
	// Create a temp map to avoid locking the add/remove
	pending := []*XdsConnection{}
	for _, v := range adsClients {
		pending = append(pending, v)
	}
	adsClientsMutex.RUnlock()

	// This will trigger recomputing the config for each connected Envoy.
	// It will include sending all configs that envoy is listening for, including EDS.
	// TODO: get service, serviceinstances, configs once, to avoid repeated redundant calls.
	// TODO: indicate the specific events, to only push what changed.

	pendingPush := int32(len(pending))

	tstart := time.Now()
	// Will keep trying to push to sidecars until another push starts.
	for {
		if len(pending) == 0 {
			adsLog.Infof("PushAll done %s %v", version, time.Since(tstart))
			return
		}
		currentVersion := versionInfo()
		// Stop attempting to push
		if !allowConcurrentPush && version != currentVersion {
			adsLog.Infof("PushAll abort %s, push with newer version %s in waiprogress %v", version, currentVersion, time.Since(tstart))
			return
		}

		// Using non-blocking push has problems if 2 pushes happen too close to each other
		client := pending[0]
		pending = pending[1:]

		// TODO: this should be in a thread group, to do multiple pushes in parallel.
		to := time.After(pushTimeout)
		select {
		case client.pushChannel <- &XdsEvent{
			push:    pushContext,
			pending: &pendingPush,
			version: version,
		}:
			client.LastPush = time.Now()
			client.LastPushFailure = timeZero
		case <-client.doneChannel: // connection was closed
			adsLog.Infof("Client closed connection %v", client.ConID)
		case <-to:
			pushTimeouts.Add(1)
			//default:
			// This may happen to some clients if the other side is in a bad state and can't receive.
			// The tests were catching this - one of the client was not reading.

			pending = append(pending, client)

			if client.LastPushFailure.IsZero() {
				client.LastPushFailure = time.Now()
				adsLog.Warnf("Failed to push, client busy %d %s", len(pending), client.ConID)
				pushErrors.With(prometheus.Labels{"type": "short"}).Add(1)
			} else {
				if time.Since(client.LastPushFailure) > 10*time.Second {
					adsLog.Warnf("Repeated failure to push %d %s", len(pending), client.ConID)
					// unfortunately grpc go doesn't allow closing (unblocking) the stream.
					pushErrors.With(prometheus.Labels{"type": "long"}).Add(1)
				}
			}
		}
	}
}

func (s *DiscoveryServer) addCon(conID string, con *XdsConnection) {
	adsClientsMutex.Lock()
	defer adsClientsMutex.Unlock()
	adsClients[conID] = con
	xdsClients.Set(float64(len(adsClients)))
	if con.modelNode != nil {
		if _, ok := adsSidecarIDConnectionsMap[con.modelNode.ID]; !ok {
			adsSidecarIDConnectionsMap[con.modelNode.ID] = map[string]*XdsConnection{conID: con}
		} else {
			adsSidecarIDConnectionsMap[con.modelNode.ID][conID] = con
		}
	}
}

func (s *DiscoveryServer) removeCon(conID string, con *XdsConnection) {
	adsClientsMutex.Lock()
	defer adsClientsMutex.Unlock()

	for _, c := range con.Clusters {
		s.removeEdsCon(c, conID, con)
	}

	if adsClients[conID] == nil {
		adsLog.Errorf("ADS: Removing connection for non-existing node %v.", s)
	}
	delete(adsClients, conID)
	xdsClients.Set(float64(len(adsClients)))
	if con.modelNode != nil {
		delete(adsSidecarIDConnectionsMap[con.modelNode.ID], conID)
	}
}

// send with timeout
func (conn *XdsConnection) send(res *xdsapi.DiscoveryResponse) error {
	errCh := make(chan error, 1)

	conn.ack.Lock()
	conn.ackChannels[res.Nonce] = make(chan struct{})
	ackCh := conn.ackChannels[res.Nonce]
	conn.ack.Unlock()

	defer func() {
		conn.ack.Lock()
		defer conn.ack.Unlock()
		if _, ok := conn.ackChannels[res.Nonce]; ok {
			delete(conn.ackChannels, res.Nonce)
		}
	}()

	// hardcoded for now - not sure if we need a setting
	t := time.NewTimer(ackTimeout)
	defer t.Stop()

	go func() {
		if err := conn.stream.Send(res); err != nil {
			errCh <- err
		}

		conn.mu.Lock()
		switch res.TypeUrl {
		case ClusterType:
			conn.ClusterNonceSent = res.Nonce
		case ListenerType:
			conn.ListenerNonceSent = res.Nonce
		case RouteType:
			conn.RouteNonceSent = res.Nonce
		case EndpointType:
			conn.EndpointNonceSent = res.Nonce
		}
		conn.mu.Unlock()
	}()
	select {
	case <-t.C:
		writeTimeout.Add(1)
		return fmt.Errorf("timeout waiting for ack %s, nonce %s", conn.ConID, res.Nonce)
	case err, _ := <-errCh:
		return err
	case <-ackCh:
		return nil
	}
}

func (conn *XdsConnection) notifyAck(nonce string) {
	conn.ack.Lock()
	defer conn.ack.Unlock()
	if ackCh, ok := conn.ackChannels[nonce]; ok {
		close(ackCh)
		delete(conn.ackChannels, nonce)
	}
}
