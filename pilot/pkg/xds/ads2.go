package xds

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"

	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/spiffe"
	istiolog "istio.io/pkg/log"
)

type XdsServer struct {
	InitializeStream func()
	IsServerReady    func() bool
	Authenticate     func(ctx context.Context) ([]string, error)

	ProcessRequest func(req *discovery.DiscoveryRequest, con *Connection) error
	PushConnection func(con *Connection, pushRequest *model.PushRequest) error

	// Generators allow customizing the generated config, based on the client metadata.
	// Key is the generator type - will match the Generator metadata to set the per-connection
	// default generator, or the combination of Generator metadata and TypeUrl to select a
	// different generator for a type.
	// Normal istio clients use the default generator - will not be impacted by this.
	Generators map[string]model.XdsResourceGenerator

	// adsClients reflect active gRPC channels, for both ADS and EDS.
	adsClients      map[string]*Connection
	adsClientsMutex sync.RWMutex
}

// StreamAggregatedResources implements the ADS interface.
func (s *XdsServer) Stream(stream DiscoveryStream) error {
	// Check if server is ready to accept clients and process new requests.
	// Currently ready means caches have been synced and hence can build
	// clusters correctly. Without this check, InitContext() call below would
	// initialize with empty config, leading to reconnected Envoys loosing
	// configuration. This is an additional safety check inaddition to adding
	// cachesSynced logic to readiness probe to handle cases where kube-proxy
	// ip tables update latencies.
	// See https://github.com/istio/istio/issues/25495.
	if !s.IsServerReady() {
		return errors.New("server is not ready to serve discovery information")
	}

	ctx := stream.Context()
	peerAddr := "0.0.0.0"
	if peerInfo, ok := peer.FromContext(ctx); ok {
		peerAddr = peerInfo.Addr.String()
	}

	ids, err := s.Authenticate(ctx)
	if err != nil {
		return err
	}
	if ids != nil {
		adsLog.Debugf("Authenticated XDS: %v with identity %v", peerAddr, ids)
	} else {
		adsLog.Debuga("Unauthenticated XDS: ", peerAddr)
	}

	s.InitializeStream()
	con := newConnection(peerAddr, stream)
	con.Identities = ids

	// Do not call: defer close(con.pushChannel). The push channel will be garbage collected
	// when the connection is no longer used. Closing the channel can cause subtle race conditions
	// with push. According to the spec: "It's only necessary to close a channel when it is important
	// to tell the receiving goroutines that all data have been sent."

	// Reading from a stream is a blocking operation. Each connection needs to read
	// discovery requests and wait for push commands on config change, so we add a
	// go routine. If go grpc adds gochannel support for streams this will not be needed.
	// This also detects close.
	var receiveError error
	reqChannel := make(chan *discovery.DiscoveryRequest, 1)
	go s.receive(con, reqChannel, &receiveError)

	for {
		// Block until either a request is received or a push is triggered.
		// We need 2 go routines because 'read' blocks in Recv().
		//
		// To avoid 2 routines, we tried to have Recv() in StreamAggregateResource - and the push
		// on different short-lived go routines started when the push is happening. This would cut in 1/2
		// the number of long-running go routines, since push is throttled. The main problem is with
		// closing - the current gRPC library didn't allow closing the stream.
		select {
		case req, ok := <-reqChannel:
			if !ok {
				// Remote side closed connection or error processing the request.
				return receiveError
			}
			// processRequest is calling pushXXX, accessing common structs with pushConnection.
			// Adding sync is the second issue to be resolved if we want to save 1/2 of the threads.
			err := s.processRequest(req, con)
			if err != nil {
				return err
			}

		case pushEv := <-con.pushChannel:
			// TODO: possible race condition: if a config change happens while the envoy
			// was getting the initial config, between LDS and RDS, the push will miss the
			// monitored 'routes'. Same for CDS/EDS interval. It is very tricky to handle
			// due to the protocol - but the periodic push recovers from it.
			err := s.pushConnection(con, pushEv.pushRequest)
			pushEv.done()
			if err != nil {
				return err
			}
		case <-con.stop:
			return nil
		}
	}
}

func (s *XdsServer) receive(con *Connection, reqChannel chan *discovery.DiscoveryRequest, errP *error) {
	defer close(reqChannel) // indicates close of the remote side.
	firstReq := true
	for {
		req, err := con.stream.Recv()
		if err != nil {
			if isExpectedGRPCError(err) {
				adsLog.Infof("ADS: %q %s terminated %v", con.PeerAddr, con.ConID, err)
				return
			}
			*errP = err
			adsLog.Errorf("ADS: %q %s terminated with error: %v", con.PeerAddr, con.ConID, err)
			totalXDSInternalErrors.Increment()
			return
		}
		// This should be only set for the first request. The node id may not be set - for example malicious clients.
		if firstReq {
			firstReq = false
			if req.Node == nil || req.Node.Id == "" {
				*errP = errors.New("missing node ID")
				return
			}
			// TODO: We should validate that the namespace in the cert matches the claimed namespace in metadata.
			if err := s.initConnection(req.Node, con); err != nil {
				*errP = err
				return
			}
			adsLog.Infof("ADS: new connection for node:%s", con.ConID)
			defer func() {
				s.removeCon(con.ConID)
				//if s.InternalGen != nil {
				//	s.InternalGen.OnDisconnect(con)
				//}
			}()
		}

		select {
		case reqChannel <- req:
		case <-con.stream.Context().Done():
			adsLog.Infof("ADS: %q %s terminated with stream closed", con.PeerAddr, con.ConID)
			return
		}
	}
}

// update the node associated with the connection, after receiving a a packet from envoy, also adds the connection
// to the tracking map.
func (s *XdsServer) initConnection(node *core.Node, con *Connection) error {
	proxy, err := s.initProxy(node, con)
	if err != nil {
		return err
	}

	// Based on node metadata and version, we can associate a different generator.
	// TODO: use a map of generators, so it's easily customizable and to avoid deps
	con.proxy.WatchedResources = map[string]*model.WatchedResource{}

	if proxy.Metadata.Generator != "" {
		proxy.XdsResourceGenerator = s.Generators[proxy.Metadata.Generator]
	}

	// First request so initialize connection id and start tracking it.
	con.proxy = proxy
	con.ConID = connectionID(node.Id)
	con.node = node

	if features.EnableXDSIdentityCheck && con.Identities != nil {
		// TODO: allow locking down, rejecting unauthenticated requests.
		id, err := checkConnectionIdentity(con)
		if err != nil {
			adsLog.Warnf("Unauthorized XDS: %v with identity %v: %v", con.PeerAddr, con.Identities, err)
			return fmt.Errorf("authorization failed: %v", err)
		}
		con.proxy.VerifiedIdentity = id
	}

	s.addCon(con.ConID, con)

	//if s.InternalGen != nil {
	//	s.InternalGen.OnConnect(con)
	//}

	return nil
}

// initProxy initializes the Proxy from node.
func (s *XdsServer) initProxy(node *core.Node, con *Connection) (*model.Proxy, error) {
	meta, err := model.ParseMetadata(node.Metadata)
	if err != nil {
		return nil, err
	}
	proxy, err := model.ParseServiceNodeWithMetadata(node.Id, meta)
	if err != nil {
		return nil, err
	}
	// Update the config namespace associated with this proxy
	proxy.ConfigNamespace = model.GetProxyConfigNamespace(proxy)

	// this should be done before we look for service instances, but after we load metadata
	// TODO fix check in kubecontroller treat echo VMs like there isn't a pod
	//s.InternalGen.RegisterWorkload(proxy, con)
	//
	//s.setProxyState(proxy, s.globalPushContext())

	// Get the locality from the proxy's service instances.
	// We expect all instances to have the same IP and therefore the same locality.
	// So its enough to look at the first instance.
	if len(proxy.ServiceInstances) > 0 {
		proxy.Locality = util.ConvertLocality(proxy.ServiceInstances[0].Endpoint.Locality.Label)
	}

	// If there is no locality in the registry then use the one sent as part of the discovery request.
	// This is not preferable as only the connected Pilot is aware of this proxies location, but it
	// can still help provide some client-side Envoy context when load balancing based on location.
	if util.IsLocalityEmpty(proxy.Locality) {
		proxy.Locality = &core.Locality{
			Region:  node.Locality.GetRegion(),
			Zone:    node.Locality.GetZone(),
			SubZone: node.Locality.GetSubZone(),
		}
	}

	// Discover supported IP Versions of proxy so that appropriate config can be delivered.
	proxy.DiscoverIPVersions()

	return proxy, nil
}

func (s *XdsServer) addCon(conID string, con *Connection) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()
	s.adsClients[conID] = con
	recordXDSClients(con.proxy.Metadata.IstioVersion, 1)
}

func (s *XdsServer) removeCon(conID string) {
	s.adsClientsMutex.Lock()
	defer s.adsClientsMutex.Unlock()

	if con, exist := s.adsClients[conID]; !exist {
		adsLog.Errorf("ADS: Removing connection for non-existing node:%v.", conID)
		totalXDSInternalErrors.Increment()
	} else {
		delete(s.adsClients, conID)
		recordXDSClients(con.proxy.Metadata.IstioVersion, -1)
	}

	//if s.StatusReporter != nil {
	//	s.StatusReporter.RegisterDisconnect(conID, AllEventTypesList)
	//}
}
