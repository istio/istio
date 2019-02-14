package v2

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	ads "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v2"
	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
)

func newDeltaXdsConnection(peerAddr string, stream DeltaDiscoveryStream) *XdsConnection {
	return &XdsConnection{
		pushChannel:  make(chan *XdsEvent),
		PeerAddr:     peerAddr,
		Clusters:     []string{},
		Connect:      time.Now(),
		deltaStream:  stream,
		LDSListeners: []*xdsapi.Listener{},
		RouteConfigs: map[string]*xdsapi.RouteConfiguration{},
	}
}

//DeltaDiscoveryStream represents the GRPC connection for any delta streams.
type DeltaDiscoveryStream interface {
	Send(*xdsapi.DeltaDiscoveryResponse) error
	Recv() (*xdsapi.DeltaDiscoveryRequest, error)
	grpc.ServerStream
}

// update the node associated with the connection, after receiving a a packet from envoy.
func (s *DiscoveryServer) initDeltaConnectionNode(discReq *xdsapi.DeltaDiscoveryRequest, con *XdsConnection) error {
	con.mu.RLock() // may not be needed - once per connection, but locking for consistency.
	if con.modelNode != nil {
		con.mu.RUnlock()
		return nil // only need to init the node on first request in the stream
	}
	con.mu.RUnlock()

	if discReq.Node == nil || discReq.Node.Id == "" {
		return errors.New("missing node id")
	}
	nt, err := model.ParseServiceNodeWithMetadata(discReq.Node.Id, model.ParseMetadata(discReq.Node.Metadata))
	if err != nil {
		return err
	}
	// Update the config namespace associated with this proxy
	nt.ConfigNamespace = model.GetProxyConfigNamespace(nt)
	nt.Locality = discReq.Node.Locality

	if err := nt.SetServiceInstances(s.Env); err != nil {
		return err
	}

	if util.IsLocalityEmpty(nt.Locality) {
		// Get the locality from the proxy's service instances.
		// We expect all instances to have the same locality. So its enough to look at the first instance
		if len(nt.ServiceInstances) > 0 {
			nt.Locality = util.ConvertLocality(nt.ServiceInstances[0].GetLocality())
		}
	}

	// Set the sidecarScope associated with this proxy if its a sidecar.
	if nt.Type == model.SidecarProxy {
		nt.SetSidecarScope(s.globalPushContext())
	}

	con.mu.Lock()
	con.modelNode = nt
	if con.ConID == "" {
		// first request
		con.ConID = connectionID(discReq.Node.Id)
	}
	con.mu.Unlock()

	return nil
}

func receiveDeltaXdsThread(con *XdsConnection, reqChannel chan *xdsapi.DeltaDiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	for {
		req, err := con.deltaStream.Recv()
		if err != nil {
			if status.Code(err) == codes.Canceled || err == io.EOF {
				con.mu.RLock()
				adsLog.Infof("ADS: %q %s terminated %v", con.PeerAddr, con.ConID, err)
				con.mu.RUnlock()
				return
			}
			*errP = err
			adsLog.Errorf("ADS: %q %s terminated with errors %v", con.PeerAddr, con.ConID, err)
			totalXDSInternalErrors.Add(1)
			return
		}
		select {
		case reqChannel <- req:
		case <-con.deltaStream.Context().Done():
			adsLog.Errorf("ADS: %q %s terminated with stream closed", con.PeerAddr, con.ConID)
			return
		}
	}
}

// DeltaAggregatedResources implements the delta xDS ADS interface.
func (s *DiscoveryServer) DeltaAggregatedResources(stream ads.AggregatedDiscoveryService_DeltaAggregatedResourcesServer) error {
	peerInfo, ok := peer.FromContext(stream.Context())
	peerAddr := "0.0.0.0"
	if ok {
		peerAddr = peerInfo.Addr.String()
	}

	//Bavery_QUESTION: Multiple Pilot instances? How to handle shared list?

	// rate limit the herd, after restart all endpoints will reconnect to the
	// poor new pilot and overwhelm it.
	// TODO: instead of readiness probe, let endpoints connect and wait here for
	// config to become stable. Will better spread the load.
	s.initRateLimiter.Wait(context.TODO())

	// first call - lazy loading, in tests. This should not happen if readiness
	// check works, since it assumes ClearCache is called (and as such PushContext
	// is initialized)
	// InitContext returns immediately if the context was already initialized.
	err := s.globalPushContext().InitContext(s.Env)
	if err != nil {
		// Error accessing the data - log and close, maybe a different pilot replica
		// has more luck
		adsLog.Warnf("Error reading config %v", err)
		return err
	}
	con := newDeltaXdsConnection(peerAddr, stream)

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
	reqChannel := make(chan *xdsapi.DeltaDiscoveryRequest, 1)
	go receiveDeltaXdsThread(con, reqChannel, &receiveError)

	for {
		// Block until either a request is received or a push is triggered.
		select {
		case discReq, ok := <-reqChannel:
			if !ok {
				// Remote side closed connection.
				return receiveError
			}
			err = s.initDeltaConnectionNode(discReq, con)
			if err != nil {
				return err
			}

			switch discReq.TypeUrl {
			case ClusterType:
				return status.Errorf(codes.Unimplemented, "not implemented")
			case ListenerType:
				return status.Errorf(codes.Unimplemented, "not implemented")
			case RouteType:
				if con.vhdsEnabled, _ = strconv.ParseBool(con.modelNode.Metadata["ENABLE_DYNAMIC_HOST_CONFIGURATION"]); con.vhdsEnabled == true {
					fmt.Printf("BAVERY: VHDS enabled.\n")
				}
				fmt.Printf("Bavery: received RDS request. \n subscribe: %+v \n unsubscribe: %+v vhds should be: %+v\n\n", discReq.GetResourceNamesSubscribe(), discReq.GetResourceNamesUnsubscribe(), con.vhdsEnabled)
				//BAVERY_TODO: Cleanup... this is a direct copy of the other RouteType code
				if discReq.ErrorDetail != nil {
					adsLog.Warnf("ADS:RDS: ACK ERROR %v %s (%s) %v", peerAddr, con.ConID, con.modelNode.ID, discReq.String())
					rdsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
					totalXDSRejects.Add(1)
					continue
				}
				routes := discReq.GetResourceNamesSubscribe()
				var sortedRoutes []string
				if discReq.ResponseNonce != "" {
					con.mu.RLock()
					routeNonceSent := con.RouteNonceSent
					//routeVersionInfoSent := con.RouteVersionInfoSent
					con.mu.RUnlock()
					if routeNonceSent != "" && routeNonceSent != discReq.ResponseNonce {
						adsLog.Debugf("ADS:RDS: Expired nonce received %s %s (%v), sent %s, received %s",
							peerAddr, con.ConID, con.modelNode, routeNonceSent, discReq.ResponseNonce)
						rdsExpiredNonce.Inc()
						continue
					}

					//BAVERY_FIXME: Cleanup
					/*	if discReq.VersionInfo == routeVersionInfoSent {
							sort.Strings(routes)
							sortedRoutes = routes
							if reflect.DeepEqual(con.Routes, sortedRoutes) {
								adsLog.Debugf("ADS:RDS: ACK %s %s (%v) %s %s", peerAddr, con.ConID, con.modelNode, discReq.VersionInfo, discReq.ResponseNonce)
								con.mu.Lock()
								con.RouteNonceAcked = discReq.ResponseNonce
								con.mu.Unlock()
								continue
							}
						} else if discReq.ErrorDetail != nil || routes == nil {
							// If versions mismatch then we should either have an error detail or no routes if a protocol error has occurred
							if discReq.ErrorDetail != nil {
								adsLog.Warnf("ADS:RDS: ACK ERROR %v %s (%v) %v", peerAddr, con.ConID, con.modelNode, discReq.String())
								rdsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
								totalXDSRejects.Add(1)
							} else { // protocol error
								adsLog.Warnf("ADS:RDS: ACK PROTOCOL ERROR %v %s (%v) %v", peerAddr, con.ConID, con.modelNode, discReq.String())
								rdsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": "Protocol error"}).Add(1)
								totalXDSRejects.Add(1)
								continue
							}
							continue
						}*/
				}

				//BAVERY_FIXME: handle unsubscribe
				unsubscribeRoutes := discReq.GetResourceNamesUnsubscribe()

				if sortedRoutes == nil {
					sort.Strings(routes)
					sortedRoutes = routes
				}
				con.Routes = sortedRoutes
				adsLog.Debugf("ADS:RDS: REQ %s %s  routes: %d", peerAddr, con.ConID, len(con.Routes))
				err := s.pushDeltaRoute(con, s.globalPushContext(), unsubscribeRoutes)
				if err != nil {
					return err
				}

			case VhdsType:
				//BAVERY_TODO: Find a better way to handle these environment variables than parsing each time
				if enableVHDS, _ := strconv.ParseBool(con.modelNode.Metadata["ENABLE_DYNAMIC_HOST_CONFIGURATION"]); !enableVHDS {
					return status.Errorf(codes.Unavailable, "VHDS not enabled.")
				}

				if discReq.ErrorDetail != nil {
					adsLog.Warnf("ADS:VHDS: ACK ERROR %v %s (%s) %v", peerAddr, con.ConID, con.modelNode.ID, discReq.String())
					vhdsReject.With(prometheus.Labels{"node": discReq.Node.Id, "err": discReq.ErrorDetail.Message}).Add(1)
					totalXDSRejects.Add(1)
					continue
				}

				//BAVERY_TODO: Complete
				//currentHosts := []string{}
				subscribeHosts := discReq.GetResourceNamesSubscribe()
				sort.Strings(subscribeHosts)
				unsubscribeHosts := discReq.GetResourceNamesUnsubscribe()
				sort.Strings(unsubscribeHosts)
				//mergeSubscribeUnsubsribe(currentHosts, subscribeHosts, unsubscribeHosts)
				fmt.Printf("Bavery vhds request received. \n Subscribing to: %+v \n unsubscribing from: %+v\n\n\n", subscribeHosts, unsubscribeHosts)

				con.mu.RLock()
				vHostNonceSent := con.VHostNonceSent
				con.mu.RUnlock()

				if vHostNonceSent != "" && vHostNonceSent != discReq.ResponseNonce {
					adsLog.Debugf("ADS:RDS: Expired nonce received %s %s (%v), sent %s, received %s",
						peerAddr, con.ConID, con.modelNode, vHostNonceSent, discReq.ResponseNonce)
					vhdsExpiredNonce.Inc()
					continue
				}

				//BAVERY_TODO: Add ErrorDetail
				//BAVERY_QUESTION: Do we support namespace in the path? Probably not.

				for _, subscribeVhost := range subscribeHosts {
					routeName, host, err := splitVHost(subscribeVhost)
					if err != nil {
						adsLog.Errorf("Request received with invalid route config name: %+v.", err.Error())
					} else {
						//use wildcard namespace to select the service from any available namespace. We likely can't get the actual namespace from Envoy.
						sscope, err := subscribeToVHost(con.modelNode.SidecarScope, s.globalPushContext(), routeName, "*/"+host, "*")
						if err != nil {
							fmt.Printf("Failed to subscribe to vhost: %s", err.Error())
							continue
						}
						con.modelNode.SidecarScope = sscope
					}
				}

				err := s.pushDeltaVirtualHost(con, s.globalPushContext(), unsubscribeHosts)
				if err != nil {
					return err
				}

			case EndpointType:
				return status.Errorf(codes.Unimplemented, "not implemented")
			default:
				adsLog.Warnf("ADS: Unknown watched resources %s", discReq.String())
			}

			if !con.added {
				con.added = true
				s.addCon(con.ConID, con)
				defer s.removeCon(con.ConID, con)
			}
		case pushEv := <-con.pushChannel:
			// It is called when config changes.
			// This is not optimized yet - we should detect what changed based on event and only
			// push resources that need to be pushed.

			// TODO: possible race condition: if a config change happens while the envoy
			// was getting the initial config, between LDS and RDS, the push will miss the
			// monitored 'routes'. Same for CDS/EDS interval.
			// It is very tricky to handle due to the protocol - but the periodic push recovers
			// from it.

			err := s.pushConnection(con, pushEv)
			if err != nil {
				return nil
			}

		}
	}
}

/*func mergeSubscribeUnsubsribe(resource []string, subscribe []string, unsubscribe []string) []string {
	for i := 0; i < len(resource); i++ {
		for j := 0; j < len(subscribe); j++ {
			for k := 0; k < len(unsubscribe); k++ {

			}
		}
	}
}*/

// Send with timeout
func (conn *XdsConnection) sendDelta(res *xdsapi.DeltaDiscoveryResponse) error {
	done := make(chan error)
	t := time.NewTimer(SendTimeout)
	go func() {
		err := conn.deltaStream.Send(res)
		done <- err
		conn.mu.Lock()
		if res.Nonce != "" {
			conn.RouteNonceSent = res.Nonce
		}
		conn.mu.Unlock()
	}()
	select {
	case <-t.C:
		// TODO: wait for ACK
		adsLog.Infof("Timeout writing %s", conn.ConID)
		xdsResponseWriteTimeouts.Add(1)
		return errors.New("timeout sending")
	case err := <-done:
		t.Stop()
		return err
	}
}

func subscribeToVHost(sidecarScope *model.SidecarScope, pushContext *model.PushContext, routeConfigName uint32, host string, namespace string) (*model.SidecarScope, error) {
	if sidecarScope == nil {
		return nil, fmt.Errorf("nil sidecar scope")
	}

	var sidecarConfig *networking.Sidecar
	var ok bool

	if sidecarScope.Config != nil {
		sidecarCfg := sidecarScope.Config
		sidecarConfig, ok = sidecarCfg.Spec.(*networking.Sidecar)
		if !ok {
			return nil, fmt.Errorf("invalid sidecar config")
		}
	} else {
		sidecarConfig = &networking.Sidecar{
			Egress: []*networking.IstioEgressListener{
				{
					//Bavery_QUESTION: This is likely wrong, but how do we figure out the port info? Specifically the protocol
					Port: &networking.Port{
						Number:   routeConfigName,
						Protocol: "HTTP",
					},
				},
			},
		}

	}

	found := false

	for _, egressPb := range sidecarConfig.Egress {
		if egressPb.Port.Number != uint32(routeConfigName) {
			continue
		}
		found = true
		egressPb.Hosts = append(egressPb.Hosts, host)
		break
	}
	if !found {
		sidecarConfig.Egress = append(sidecarConfig.Egress, &networking.IstioEgressListener{
			Port: &networking.Port{
				Number:   routeConfigName,
				Protocol: "HTTP",
			},
		})
	}

	sdCfg := &model.Config{
		Spec: sidecarConfig,
	}
	updatedSidecarScope := model.ConvertToSidecarScope(pushContext, sdCfg, namespace)
	return updatedSidecarScope, nil
}

//splitVHost returns the vhost and route config from a string
func splitVHost(entry string) (uint32, string, error) {
	vhostSeparator := "$"
	//our entry is required to have a route config name and a vhost. If it's missing something, ignore
	if entryParts := strings.SplitN(entry, vhostSeparator, 2); len(entryParts) != 2 {
		return 0, "", fmt.Errorf("could not split entry parts")
	} else {
		routeName, err := strconv.Atoi(entryParts[0])
		if err != nil {
			return 0, "", fmt.Errorf("virtualhost contained invalid route name")
		}
		return uint32(routeName), entryParts[1], nil
	}
}
