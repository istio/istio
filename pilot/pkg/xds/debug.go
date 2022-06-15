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
	"encoding/json"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/proto"
	any "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	"istio.io/istio/pilot/pkg/serviceregistry/provider"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/config/xds"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/util/protomarshal"
	istiolog "istio.io/pkg/log"
)

var indexTmpl = template.Must(template.New("index").Parse(`<html>
<head>
<title>Pilot Debug Console</title>
</head>
<style>
#endpoints {
  font-family: "Trebuchet MS", Arial, Helvetica, sans-serif;
  border-collapse: collapse;
}

#endpoints td, #endpoints th {
  border: 1px solid #ddd;
  padding: 8px;
}

#endpoints tr:nth-child(even){background-color: #f2f2f2;}

#endpoints tr:hover {background-color: #ddd;}

#endpoints th {
  padding-top: 12px;
  padding-bottom: 12px;
  text-align: left;
  background-color: black;
  color: white;
}
</style>
<body>
<br/>
<table id="endpoints">
<tr><th>Endpoint</th><th>Description</th></tr>
{{range .}}
	<tr>
	<td><a href='{{.Href}}'>{{.Name}}</a></td><td>{{.Help}}</td>
	</tr>
{{end}}
</table>
<br/>
</body>
</html>
`))

// AdsClient defines the data that is displayed on "/adsz" endpoint.
type AdsClient struct {
	ConnectionID string              `json:"connectionId"`
	ConnectedAt  time.Time           `json:"connectedAt"`
	PeerAddress  string              `json:"address"`
	Metadata     *model.NodeMetadata `json:"metadata,omitempty"`
	Watches      map[string][]string `json:"watches,omitempty"`
}

// AdsClients is collection of AdsClient connected to this Istiod.
type AdsClients struct {
	Total     int         `json:"totalClients"`
	Connected []AdsClient `json:"clients,omitempty"`
}

// SyncStatus is the synchronization status between Pilot and a given Envoy
type SyncStatus struct {
	ClusterID            string `json:"cluster_id,omitempty"`
	ProxyID              string `json:"proxy,omitempty"`
	ProxyVersion         string `json:"proxy_version,omitempty"`
	IstioVersion         string `json:"istio_version,omitempty"`
	ClusterSent          string `json:"cluster_sent,omitempty"`
	ClusterAcked         string `json:"cluster_acked,omitempty"`
	ListenerSent         string `json:"listener_sent,omitempty"`
	ListenerAcked        string `json:"listener_acked,omitempty"`
	RouteSent            string `json:"route_sent,omitempty"`
	RouteAcked           string `json:"route_acked,omitempty"`
	EndpointSent         string `json:"endpoint_sent,omitempty"`
	EndpointAcked        string `json:"endpoint_acked,omitempty"`
	ExtensionConfigSent  string `json:"extensionconfig_sent,omitempty"`
	ExtensionConfigAcked string `json:"extensionconfig_acked,omitempty"`
}

// SyncedVersions shows what resourceVersion of a given resource has been acked by Envoy.
type SyncedVersions struct {
	ProxyID         string `json:"proxy,omitempty"`
	ClusterVersion  string `json:"cluster_acked,omitempty"`
	ListenerVersion string `json:"listener_acked,omitempty"`
	RouteVersion    string `json:"route_acked,omitempty"`
}

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller, enableProfiling bool,
	fetchWebhook func() map[string]string,
) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = memory.NewServiceDiscovery()
	s.MemRegistry.EDSUpdater = s
	s.MemRegistry.ClusterID = "v2-debug"

	sctl.AddRegistry(serviceregistry.Simple{
		ClusterID:        "v2-debug",
		ProviderID:       provider.Mock,
		ServiceDiscovery: s.MemRegistry,
		Controller:       s.MemRegistry.Controller,
	})
	internalMux := http.NewServeMux()
	s.AddDebugHandlers(mux, internalMux, enableProfiling, fetchWebhook)
	debugGen, ok := (s.Generators[TypeDebug]).(*DebugGen)
	if ok {
		debugGen.DebugMux = internalMux
	}
}

func (s *DiscoveryServer) AddDebugHandlers(mux, internalMux *http.ServeMux, enableProfiling bool, webhook func() map[string]string) {
	// Debug handlers on HTTP ports are added for backward compatibility.
	// They will be exposed on XDS-over-TLS in future releases.
	if !features.EnableDebugOnHTTP {
		return
	}

	if enableProfiling {
		s.addDebugHandler(mux, internalMux, "/debug/pprof/", "Displays pprof index", pprof.Index)
		s.addDebugHandler(mux, internalMux, "/debug/pprof/cmdline", "The command line invocation of the current program", pprof.Cmdline)
		s.addDebugHandler(mux, internalMux, "/debug/pprof/profile", "CPU profile", pprof.Profile)
		s.addDebugHandler(mux, internalMux, "/debug/pprof/symbol", "Symbol looks up the program counters listed in the request", pprof.Symbol)
		s.addDebugHandler(mux, internalMux, "/debug/pprof/trace", "A trace of execution of the current program.", pprof.Trace)
	}

	mux.HandleFunc("/debug", s.Debug)

	if features.EnableUnsafeAdminEndpoints {
		s.addDebugHandler(mux, internalMux, "/debug/force_disconnect", "Disconnects a proxy from this Pilot", s.forceDisconnect)
	}

	s.addDebugHandler(mux, internalMux, "/debug/ecdsz", "Status and debug interface for ECDS", s.ecdsz)
	s.addDebugHandler(mux, internalMux, "/debug/edsz", "Status and debug interface for EDS", s.Edsz)
	s.addDebugHandler(mux, internalMux, "/debug/ndsz", "Status and debug interface for NDS", s.ndsz)
	s.addDebugHandler(mux, internalMux, "/debug/adsz", "Status and debug interface for ADS", s.adsz)
	s.addDebugHandler(mux, internalMux, "/debug/adsz?push=true", "Initiates push of the current state to all connected endpoints", s.adsz)

	s.addDebugHandler(mux, internalMux, "/debug/syncz", "Synchronization status of all Envoys connected to this Pilot instance", s.Syncz)
	s.addDebugHandler(mux, internalMux, "/debug/config_distribution", "Version status of all Envoys connected to this Pilot instance", s.distributedVersions)

	s.addDebugHandler(mux, internalMux, "/debug/registryz", "Debug support for registry", s.registryz)
	s.addDebugHandler(mux, internalMux, "/debug/endpointz", "Debug support for endpoints", s.endpointz)
	s.addDebugHandler(mux, internalMux, "/debug/endpointShardz", "Info about the endpoint shards", s.endpointShardz)
	s.addDebugHandler(mux, internalMux, "/debug/cachez", "Info about the internal XDS caches", s.cachez)
	s.addDebugHandler(mux, internalMux, "/debug/cachez?sizes=true", "Info about the size of the internal XDS caches", s.cachez)
	s.addDebugHandler(mux, internalMux, "/debug/cachez?clear=true", "Clear the XDS caches", s.cachez)
	s.addDebugHandler(mux, internalMux, "/debug/configz", "Debug support for config", s.configz)
	s.addDebugHandler(mux, internalMux, "/debug/sidecarz", "Debug sidecar scope for a proxy", s.sidecarz)
	s.addDebugHandler(mux, internalMux, "/debug/resourcesz", "Debug support for watched resources", s.resourcez)
	s.addDebugHandler(mux, internalMux, "/debug/instancesz", "Debug support for service instances", s.instancesz)

	s.addDebugHandler(mux, internalMux, "/debug/authorizationz", "Internal authorization policies", s.authorizationz)
	s.addDebugHandler(mux, internalMux, "/debug/telemetryz", "Debug Telemetry configuration", s.telemetryz)
	s.addDebugHandler(mux, internalMux, "/debug/config_dump", "ConfigDump in the form of the Envoy admin config dump API for passed in proxyID", s.ConfigDump)
	s.addDebugHandler(mux, internalMux, "/debug/push_status", "Last PushContext Details", s.pushStatusHandler)
	s.addDebugHandler(mux, internalMux, "/debug/pushcontext", "Debug support for current push context", s.pushContextHandler)
	s.addDebugHandler(mux, internalMux, "/debug/connections", "Info about the connected XDS clients", s.connectionsHandler)

	s.addDebugHandler(mux, internalMux, "/debug/inject", "Active inject template", s.injectTemplateHandler(webhook))
	s.addDebugHandler(mux, internalMux, "/debug/mesh", "Active mesh config", s.meshHandler)
	s.addDebugHandler(mux, internalMux, "/debug/clusterz", "List remote clusters where istiod reads endpoints", s.clusterz)
	s.addDebugHandler(mux, internalMux, "/debug/networkz", "List cross-network gateways", s.networkz)
	s.addDebugHandler(mux, internalMux, "/debug/mcsz", "List information about Kubernetes MCS services", s.mcsz)

	s.addDebugHandler(mux, internalMux, "/debug/list", "List all supported debug commands in json", s.List)
}

func (s *DiscoveryServer) addDebugHandler(mux *http.ServeMux, internalMux *http.ServeMux,
	path string, help string, handler func(http.ResponseWriter, *http.Request),
) {
	s.debugHandlers[path] = help
	// Add handler without auth. This mux is never exposed on an HTTP server and only used internally
	if internalMux != nil {
		internalMux.HandleFunc(path, handler)
	}
	// Add handler with auth; this is expose on an HTTP server
	mux.HandleFunc(path, s.allowAuthenticatedOrLocalhost(http.HandlerFunc(handler)))
}

func (s *DiscoveryServer) allowAuthenticatedOrLocalhost(next http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		// Request is from localhost, no need to authenticate
		if isRequestFromLocalhost(req) {
			next.ServeHTTP(w, req)
			return
		}
		// Authenticate request with the same method as XDS
		authFailMsgs := make([]string, 0)
		var ids []string
		for _, authn := range s.Authenticators {
			u, err := authn.AuthenticateRequest(req)
			// If one authenticator passes, return
			if u != nil && u.Identities != nil && err == nil {
				ids = u.Identities
				break
			}
			authFailMsgs = append(authFailMsgs, fmt.Sprintf("Authenticator %s: %v", authn.AuthenticatorType(), err))
		}
		if ids == nil {
			istiolog.Errorf("Failed to authenticate %s %v", req.URL, authFailMsgs)
			// Not including detailed info in the response, XDS doesn't either (returns a generic "authentication failure).
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		// TODO: Check that the identity contains istio-system namespace, else block or restrict to only info that
		// is visible to the authenticated SA. Will require changes in docs and istioctl too.
		next.ServeHTTP(w, req)
	}
}

func isRequestFromLocalhost(r *http.Request) bool {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}

	userIP := net.ParseIP(ip)
	return userIP.IsLoopback()
}

// Syncz dumps the synchronization status of all Envoys connected to this Pilot instance
func (s *DiscoveryServer) Syncz(w http.ResponseWriter, _ *http.Request) {
	syncz := make([]SyncStatus, 0)
	for _, con := range s.Clients() {
		node := con.proxy
		if node != nil {
			syncz = append(syncz, SyncStatus{
				ProxyID:              node.ID,
				ClusterID:            node.Metadata.ClusterID.String(),
				IstioVersion:         node.Metadata.IstioVersion,
				ClusterSent:          con.NonceSent(v3.ClusterType),
				ClusterAcked:         con.NonceAcked(v3.ClusterType),
				ListenerSent:         con.NonceSent(v3.ListenerType),
				ListenerAcked:        con.NonceAcked(v3.ListenerType),
				RouteSent:            con.NonceSent(v3.RouteType),
				RouteAcked:           con.NonceAcked(v3.RouteType),
				EndpointSent:         con.NonceSent(v3.EndpointType),
				EndpointAcked:        con.NonceAcked(v3.EndpointType),
				ExtensionConfigSent:  con.NonceSent(v3.ExtensionConfigurationType),
				ExtensionConfigAcked: con.NonceAcked(v3.ExtensionConfigurationType),
			})
		}
	}
	writeJSON(w, syncz)
}

// registryz providees debug support for registry - adding and listing model items.
// Can be combined with the push debug interface to reproduce changes.
func (s *DiscoveryServer) registryz(w http.ResponseWriter, req *http.Request) {
	all := s.Env.ServiceDiscovery.Services()
	writeJSON(w, all)
}

// Dumps info about the endpoint shards, tracked using the new direct interface.
// Legacy registry provides are synced to the new data structure as well, during
// the full push.
func (s *DiscoveryServer) endpointShardz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	out, _ := json.MarshalIndent(s.EndpointIndex.Shardz(), " ", " ")
	_, _ = w.Write(out)
}

func (s *DiscoveryServer) cachez(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Failed to parse request\n"))
		return
	}
	if req.Form.Get("clear") != "" {
		s.Cache.ClearAll()
		_, _ = w.Write([]byte("Cache cleared\n"))
		return
	}
	if req.Form.Get("sizes") != "" {
		snapshot := s.Cache.Snapshot()
		res := make(map[string]string, len(snapshot))
		totalSize := 0
		for _, resource := range snapshot {
			if resource == nil {
				continue
			}
			resourceType := resource.Resource.TypeUrl
			sz := len(resource.Resource.GetValue())
			res[resourceType] += util.ByteCount(sz)
			totalSize += sz
		}
		res["total"] = util.ByteCount(totalSize)
		writeJSON(w, res)
		return
	}
	snapshot := s.Cache.Snapshot()
	resources := make(map[string][]string, len(snapshot)) // Key is typeUrl and value is resource names.
	for key, resource := range snapshot {
		if resource == nil {
			continue
		}
		resourceType := resource.Resource.TypeUrl
		resources[resourceType] = append(resources[resourceType], resource.Name+"/"+key)
	}
	writeJSON(w, resources)
}

type endpointzResponse struct {
	Service   string                   `json:"svc"`
	Endpoints []*model.ServiceInstance `json:"ep"`
}

// Endpoint debugging
func (s *DiscoveryServer) endpointz(w http.ResponseWriter, req *http.Request) {
	if _, f := req.URL.Query()["brief"]; f {
		svc := s.Env.ServiceDiscovery.Services()
		for _, ss := range svc {
			for _, p := range ss.Ports {
				all := s.Env.ServiceDiscovery.InstancesByPort(ss, p.Port, nil)
				for _, svc := range all {
					_, _ = fmt.Fprintf(w, "%s:%s %s:%d %v %s\n", ss.Hostname,
						p.Name, svc.Endpoint.Address, svc.Endpoint.EndpointPort, svc.Endpoint.Labels,
						svc.Endpoint.ServiceAccount)
				}
			}
		}
		return
	}

	svc := s.Env.ServiceDiscovery.Services()
	resp := make([]endpointzResponse, 0)
	for _, ss := range svc {
		for _, p := range ss.Ports {
			all := s.Env.ServiceDiscovery.InstancesByPort(ss, p.Port, nil)
			resp = append(resp, endpointzResponse{
				Service:   fmt.Sprintf("%s:%s", ss.Hostname, p.Name),
				Endpoints: all,
			})
		}
	}
	writeJSON(w, resp)
}

func (s *DiscoveryServer) distributedVersions(w http.ResponseWriter, req *http.Request) {
	if !features.EnableDistributionTracking {
		w.WriteHeader(http.StatusConflict)
		_, _ = fmt.Fprint(w, "Pilot Version tracking is disabled.  Please set the "+
			"PILOT_ENABLE_CONFIG_DISTRIBUTION_TRACKING environment variable to true to enable.")
		return
	}
	if resourceID := req.URL.Query().Get("resource"); resourceID != "" {
		proxyNamespace := req.URL.Query().Get("proxy_namespace")
		knownVersions := make(map[string]string)
		var results []SyncedVersions
		for _, con := range s.Clients() {
			// wrap this in independent scope so that panic's don't bypass Unlock...
			con.proxy.RLock()

			if con.proxy != nil && (proxyNamespace == "" || proxyNamespace == con.proxy.ConfigNamespace) {
				// read nonces from our statusreporter to allow for skipped nonces, etc.
				results = append(results, SyncedVersions{
					ProxyID: con.proxy.ID,
					ClusterVersion: s.getResourceVersion(s.StatusReporter.QueryLastNonce(con.conID, v3.ClusterType),
						resourceID, knownVersions),
					ListenerVersion: s.getResourceVersion(s.StatusReporter.QueryLastNonce(con.conID, v3.ListenerType),
						resourceID, knownVersions),
					RouteVersion: s.getResourceVersion(s.StatusReporter.QueryLastNonce(con.conID, v3.RouteType),
						resourceID, knownVersions),
				})
			}
			con.proxy.RUnlock()
		}

		writeJSON(w, results)
	} else {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprintf(w, "querystring parameter 'resource' is required\n")
	}
}

// VersionLen is the Config Version and is only used as the nonce prefix, but we can reconstruct
// it because is is a b64 encoding of a 64 bit array, which will always be 12 chars in length.
// len = ceil(bitlength/(2^6))+1
const VersionLen = 12

func (s *DiscoveryServer) getResourceVersion(nonce, key string, cache map[string]string) string {
	if len(nonce) < VersionLen {
		return ""
	}
	configVersion := nonce[:VersionLen]
	result, ok := cache[configVersion]
	if !ok {
		lookupResult, err := s.Env.GetLedger().GetPreviousValue(configVersion, key)
		if err != nil {
			istiolog.Errorf("Unable to retrieve resource %s at version %s: %v", key, configVersion, err)
			lookupResult = ""
		}
		// update the cache even on an error, because errors will not resolve themselves, and we don't want to
		// repeat the same error for many s.adsClients.
		cache[configVersion] = lookupResult
		return lookupResult
	}
	return result
}

// kubernetesConfig wraps a config.Config with a custom marshaling method that matches a Kubernetes
// object structure.
type kubernetesConfig struct {
	config.Config
}

func (k kubernetesConfig) MarshalJSON() ([]byte, error) {
	cfg, err := crd.ConvertConfig(k.Config)
	if err != nil {
		return nil, err
	}
	return json.Marshal(cfg)
}

// Config debugging.
func (s *DiscoveryServer) configz(w http.ResponseWriter, req *http.Request) {
	configs := make([]kubernetesConfig, 0)
	s.Env.ConfigStore.Schemas().ForEach(func(schema collection.Schema) bool {
		cfg, _ := s.Env.ConfigStore.List(schema.Resource().GroupVersionKind(), "")
		for _, c := range cfg {
			configs = append(configs, kubernetesConfig{c})
		}
		return false
	})
	writeJSON(w, configs)
}

// SidecarScope debugging
func (s *DiscoveryServer) sidecarz(w http.ResponseWriter, req *http.Request) {
	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}
	writeJSON(w, con.proxy.SidecarScope)
}

// Resource debugging.
func (s *DiscoveryServer) resourcez(w http.ResponseWriter, _ *http.Request) {
	schemas := make([]config.GroupVersionKind, 0)
	s.Env.Schemas().ForEach(func(schema collection.Schema) bool {
		schemas = append(schemas, schema.Resource().GroupVersionKind())
		return false
	})

	writeJSON(w, schemas)
}

// AuthorizationDebug holds debug information for authorization policy.
type AuthorizationDebug struct {
	AuthorizationPolicies *model.AuthorizationPolicies `json:"authorization_policies"`
}

// authorizationz dumps the internal authorization policies.
func (s *DiscoveryServer) authorizationz(w http.ResponseWriter, req *http.Request) {
	info := AuthorizationDebug{
		AuthorizationPolicies: s.globalPushContext().AuthzPolicies,
	}
	writeJSON(w, info)
}

// AuthorizationDebug holds debug information for authorization policy.
type TelemetryDebug struct {
	Telemetries *model.Telemetries `json:"telemetries"`
}

func (s *DiscoveryServer) telemetryz(w http.ResponseWriter, req *http.Request) {
	info := TelemetryDebug{
		Telemetries: s.globalPushContext().Telemetry,
	}
	writeJSON(w, info)
}

// connectionsHandler implements interface for displaying current connections.
// It is mapped to /debug/connections.
func (s *DiscoveryServer) connectionsHandler(w http.ResponseWriter, req *http.Request) {
	adsClients := &AdsClients{}
	connections := s.Clients()
	adsClients.Total = len(connections)

	for _, c := range connections {
		adsClient := AdsClient{
			ConnectionID: c.conID,
			ConnectedAt:  c.connectedAt,
			PeerAddress:  c.peerAddr,
		}
		adsClients.Connected = append(adsClients.Connected, adsClient)
	}
	writeJSON(w, adsClients)
}

// adsz implements a status and debug interface for ADS.
// It is mapped to /debug/adsz
func (s *DiscoveryServer) adsz(w http.ResponseWriter, req *http.Request) {
	if s.handlePushRequest(w, req) {
		return
	}
	proxyID, con := s.getDebugConnection(req)
	if proxyID != "" && con == nil {
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance.\n"))
		return
	}
	var connections []*Connection
	if con != nil {
		connections = []*Connection{con}
	} else {
		connections = s.Clients()
	}

	adsClients := &AdsClients{}
	adsClients.Total = len(connections)
	for _, c := range connections {
		adsClient := AdsClient{
			ConnectionID: c.conID,
			ConnectedAt:  c.connectedAt,
			PeerAddress:  c.peerAddr,
			Metadata:     c.proxy.Metadata,
			Watches:      map[string][]string{},
		}
		c.proxy.RLock()
		for k, wr := range c.proxy.WatchedResources {
			r := wr.ResourceNames
			if r == nil {
				r = []string{}
			}
			adsClient.Watches[k] = r
		}
		c.proxy.RUnlock()
		adsClients.Connected = append(adsClients.Connected, adsClient)
	}
	sort.Slice(adsClients.Connected, func(i, j int) bool {
		return adsClients.Connected[i].ConnectionID < adsClients.Connected[j].ConnectionID
	})
	writeJSON(w, adsClients)
}

// ecdsz implements a status and debug interface for ECDS.
// It is mapped to /debug/ecdsz
func (s *DiscoveryServer) ecdsz(w http.ResponseWriter, req *http.Request) {
	if s.handlePushRequest(w, req) {
		return
	}
	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}

	if s.Generators[v3.ExtensionConfigurationType] != nil {
		r, ok := con.proxy.WatchedResources[v3.ExtensionConfigurationType]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf("no watched ExtensionConfigurationType found, proxyID: %s\n", proxyID)))
			return
		}

		resource, _, _ := s.Generators[v3.ExtensionConfigurationType].Generate(con.proxy, r, &model.PushRequest{
			Full: true,
			Push: con.proxy.LastPushContext,
		})
		if len(resource) == 0 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(fmt.Sprintf("ExtensionConfigurationType not found, proxyID: %s\n", proxyID)))
			return
		}

		wasmCfgs := make([]interface{}, 0, len(resource))
		for _, rr := range resource {
			if w, err := unmarshalToWasm(rr); err != nil {
				istiolog.Warnf("failed to unmarshal wasm: %v", err)
			} else {
				wasmCfgs = append(wasmCfgs, w)
			}
		}

		writeJSON(w, wasmCfgs)
	}
}

func unmarshalToWasm(r *discovery.Resource) (interface{}, error) {
	tce := &core.TypedExtensionConfig{}
	if err := r.GetResource().UnmarshalTo(tce); err != nil {
		return nil, err
	}

	switch tce.TypedConfig.TypeUrl {
	case xds.WasmHTTPFilterType:
		w := &wasm.Wasm{}
		if err := tce.TypedConfig.UnmarshalTo(w); err != nil {
			return nil, err
		}
		// Redact Wasm secret env variable.
		vmenvs := w.GetConfig().GetVmConfig().EnvironmentVariables
		if vmenvs != nil {
			if _, found := vmenvs.KeyValues[model.WasmSecretEnv]; found {
				vmenvs.KeyValues[model.WasmSecretEnv] = "<Redacted>"
			}
		}
		return w, nil
	}

	return tce, nil
}

// ConfigDump returns information in the form of the Envoy admin API config dump for the specified proxy
// The dump will only contain dynamic listeners/clusters/routes and can be used to compare what an Envoy instance
// should look like according to Pilot vs what it currently does look like.
func (s *DiscoveryServer) ConfigDump(w http.ResponseWriter, req *http.Request) {
	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}
	includeEds := req.URL.Query().Get("include_eds") == "true"
	dump, err := s.configDump(con, includeEds)
	if err != nil {
		handleHTTPError(w, err)
		return
	}
	writeJSON(w, dump)
}

// configDump converts the connection internal state into an Envoy Admin API config dump proto
// It is used in debugging to create a consistent object for comparison between Envoy and Pilot outputs
func (s *DiscoveryServer) configDump(conn *Connection, includeEds bool) (*adminapi.ConfigDump, error) {
	req := &model.PushRequest{Push: conn.proxy.LastPushContext, Start: time.Now(), Full: true}
	version := req.Push.PushVersion

	generate := func(typeUrl string) (model.Resources, error) {
		w := conn.Watched(typeUrl)
		if w == nil {
			// Not watched, skip
			return nil, nil
		}
		gen := s.findGenerator(typeUrl, conn)
		if gen == nil {
			// No generator found, skip
			return nil, nil
		}

		cfg, _, err := gen.Generate(conn.proxy, w, req)
		if err != nil {
			log.Warnf("failed to generate %v: %v", typeUrl, err)
		}
		return cfg, err
	}

	clusters, err := generate(v3.ClusterType)
	if err != nil {
		return nil, err
	}
	dynamicActiveClusters := make([]*adminapi.ClustersConfigDump_DynamicCluster, 0)
	for _, cs := range clusters {
		dynamicActiveClusters = append(dynamicActiveClusters, &adminapi.ClustersConfigDump_DynamicCluster{Cluster: cs.Resource})
	}
	clustersAny, err := util.MessageToAnyWithError(&adminapi.ClustersConfigDump{
		VersionInfo:           version,
		DynamicActiveClusters: dynamicActiveClusters,
	})
	if err != nil {
		return nil, err
	}

	listeners, err := generate(v3.ListenerType)
	if err != nil {
		return nil, err
	}
	dynamicActiveListeners := make([]*adminapi.ListenersConfigDump_DynamicListener, 0)
	for _, cs := range listeners {
		dynamicActiveListeners = append(dynamicActiveListeners, &adminapi.ListenersConfigDump_DynamicListener{
			Name: cs.Name,
			ActiveState: &adminapi.ListenersConfigDump_DynamicListenerState{
				Listener:    cs.Resource,
				VersionInfo: version,
			},
		})
	}
	listenersAny, err := util.MessageToAnyWithError(&adminapi.ListenersConfigDump{
		VersionInfo:      version,
		DynamicListeners: dynamicActiveListeners,
	})
	if err != nil {
		return nil, err
	}

	routes, err := generate(v3.RouteType)
	if err != nil {
		return nil, err
	}
	dynamicRouteConfig := make([]*adminapi.RoutesConfigDump_DynamicRouteConfig, 0)
	for _, cs := range routes {
		dynamicRouteConfig = append(dynamicRouteConfig, &adminapi.RoutesConfigDump_DynamicRouteConfig{
			VersionInfo: version,
			RouteConfig: cs.Resource,
		})
	}
	routesAny, err := util.MessageToAnyWithError(&adminapi.RoutesConfigDump{
		DynamicRouteConfigs: dynamicRouteConfig,
	})
	if err != nil {
		return nil, err
	}

	secrets, err := generate(v3.SecretType)
	if err != nil {
		return nil, err
	}
	dynamicSecretsConfig := make([]*adminapi.SecretsConfigDump_DynamicSecret, 0)
	for _, cs := range secrets {
		// Secrets must be redacted
		secret := &tls.Secret{}
		if err := cs.Resource.UnmarshalTo(secret); err != nil {
			istiolog.Warnf("failed to unmarshal secret: %v", err)
			continue
		}
		if secret.GetTlsCertificate() != nil {
			secret.GetTlsCertificate().PrivateKey = &core.DataSource{
				Specifier: &core.DataSource_InlineBytes{
					InlineBytes: []byte("[redacted]"),
				},
			}
		}
		dynamicSecretsConfig = append(dynamicSecretsConfig, &adminapi.SecretsConfigDump_DynamicSecret{
			VersionInfo: version,
			Secret:      util.MessageToAny(secret),
		})
	}
	secretsAny, err := util.MessageToAnyWithError(&adminapi.SecretsConfigDump{
		DynamicActiveSecrets: dynamicSecretsConfig,
	})
	if err != nil {
		return nil, err
	}

	var endpointsAny *any.Any
	// EDS is disabled by default for compatibility with Envoy config_dump interface
	if includeEds {
		endpoints, err := generate(v3.EndpointType)
		if err != nil {
			return nil, err
		}
		endpointConfig := make([]*adminapi.EndpointsConfigDump_DynamicEndpointConfig, 0)
		for _, cs := range endpoints {
			endpointConfig = append(endpointConfig, &adminapi.EndpointsConfigDump_DynamicEndpointConfig{
				VersionInfo:    version,
				EndpointConfig: cs.Resource,
			})
		}
		endpointsAny, err = util.MessageToAnyWithError(&adminapi.EndpointsConfigDump{
			DynamicEndpointConfigs: endpointConfig,
		})
		if err != nil {
			return nil, err
		}
	}

	bootstrapAny := util.MessageToAny(&adminapi.BootstrapConfigDump{})
	scopedRoutesAny := util.MessageToAny(&adminapi.ScopedRoutesConfigDump{})
	// The config dump must have all configs with connections specified in
	// https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	configs := []*any.Any{
		bootstrapAny,
		clustersAny,
	}
	if includeEds {
		configs = append(configs, endpointsAny)
	}
	configs = append(configs,
		listenersAny,
		scopedRoutesAny,
		routesAny,
		secretsAny,
	)
	configDump := &adminapi.ConfigDump{
		Configs: configs,
	}
	return configDump, nil
}

// injectTemplateHandler dumps the injection template
// Replaces dumping the template at startup.
func (s *DiscoveryServer) injectTemplateHandler(webhook func() map[string]string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		// TODO: we should split the inject template into smaller modules (separate one for dump core, etc),
		// and allow pods to select which patches will be selected. When this happen, this should return
		// all inject templates or take a param to select one.
		if webhook == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		writeJSON(w, webhook())
	}
}

// meshHandler dumps the mesh config
func (s *DiscoveryServer) meshHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Env.Mesh())
}

// pushStatusHandler dumps the last PushContext
func (s *DiscoveryServer) pushStatusHandler(w http.ResponseWriter, req *http.Request) {
	model.LastPushMutex.Lock()
	defer model.LastPushMutex.Unlock()
	if model.LastPushStatus == nil {
		return
	}
	out, err := model.LastPushStatus.StatusJSON()
	if err != nil {
		handleHTTPError(w, err)
		return
	}
	w.Header().Add("Content-Type", "application/json")

	_, _ = w.Write(out)
}

// PushContextDebug holds debug information for push context.
type PushContextDebug struct {
	AuthorizationPolicies *model.AuthorizationPolicies
	NetworkGateways       map[network.ID][]model.NetworkGateway
}

// pushContextHandler dumps the current PushContext
func (s *DiscoveryServer) pushContextHandler(w http.ResponseWriter, _ *http.Request) {
	push := PushContextDebug{
		AuthorizationPolicies: s.globalPushContext().AuthzPolicies,
		NetworkGateways:       s.globalPushContext().NetworkManager().GatewaysByNetwork(),
	}

	writeJSON(w, push)
}

// Debug lists all the supported debug endpoints.
func (s *DiscoveryServer) Debug(w http.ResponseWriter, req *http.Request) {
	type debugEndpoint struct {
		Name string
		Href string
		Help string
	}
	var deps []debugEndpoint

	for k, v := range s.debugHandlers {
		deps = append(deps, debugEndpoint{
			Name: k,
			Href: k,
			Help: v,
		})
	}

	sort.Slice(deps, func(i, j int) bool {
		return deps[i].Name < deps[j].Name
	})

	if err := indexTmpl.Execute(w, deps); err != nil {
		istiolog.Errorf("Error in rendering index template %v", err)
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// List all the supported debug commands in json.
func (s *DiscoveryServer) List(w http.ResponseWriter, req *http.Request) {
	var cmdNames []string
	for k := range s.debugHandlers {
		key := strings.Replace(k, "/debug/", "", -1)
		// exclude current list command
		if key == "list" {
			continue
		}
		// can not support pprof commands
		if strings.Contains(key, "pprof") {
			continue
		}
		cmdNames = append(cmdNames, key)
	}
	sort.Strings(cmdNames)
	writeJSON(w, cmdNames)
}

// ndsz implements a status and debug interface for NDS.
// It is mapped to /debug/ndsz on the monitor port (15014).
func (s *DiscoveryServer) ndsz(w http.ResponseWriter, req *http.Request) {
	if s.handlePushRequest(w, req) {
		return
	}
	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}
	if !con.proxy.Metadata.DNSCapture {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("DNS capture is not enabled in the proxy\n"))
		return
	}

	if s.Generators[v3.NameTableType] != nil {
		nds, _, _ := s.Generators[v3.NameTableType].Generate(con.proxy, nil, &model.PushRequest{
			Push: con.proxy.LastPushContext,
		})
		if len(nds) == 0 {
			return
		}
		writeJSON(w, nds[0])
	}
}

// Edsz implements a status and debug interface for EDS.
// It is mapped to /debug/edsz on the monitor port (15014).
func (s *DiscoveryServer) Edsz(w http.ResponseWriter, req *http.Request) {
	if s.handlePushRequest(w, req) {
		return
	}

	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}

	clusters := con.Clusters()
	eps := make([]jsonMarshalProto, 0, len(clusters))
	for _, clusterName := range clusters {
		eps = append(eps, jsonMarshalProto{s.generateEndpoints(NewEndpointBuilder(clusterName, con.proxy, con.proxy.LastPushContext))})
	}
	writeJSON(w, eps)
}

func (s *DiscoveryServer) forceDisconnect(w http.ResponseWriter, req *http.Request) {
	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}
	con.Stop()
	_, _ = w.Write([]byte("OK"))
}

func cloneProxy(proxy *model.Proxy) *model.Proxy {
	if proxy == nil {
		return nil
	}

	proxy.Lock()
	defer proxy.Unlock()
	// nolint: govet
	copied := *proxy
	out := &copied
	out.RWMutex = sync.RWMutex{}
	// clone WatchedResources which can be mutated when processing request
	out.WatchedResources = make(map[string]*model.WatchedResource, len(proxy.WatchedResources))
	for k, v := range proxy.WatchedResources {
		// nolint: govet
		v := *v
		out.WatchedResources[k] = &v
	}
	return out
}

func (s *DiscoveryServer) getProxyConnection(proxyID string) *Connection {
	for _, con := range s.Clients() {
		if strings.Contains(con.conID, proxyID) {
			out := *con
			out.proxy = cloneProxy(con.proxy)
			return &out
		}
	}

	return nil
}

func (s *DiscoveryServer) instancesz(w http.ResponseWriter, req *http.Request) {
	instances := map[string][]*model.ServiceInstance{}
	for _, con := range s.Clients() {
		con.proxy.RLock()
		if con.proxy != nil {
			instances[con.proxy.ID] = con.proxy.ServiceInstances
		}
		con.proxy.RUnlock()
	}
	writeJSON(w, instances)
}

func (s *DiscoveryServer) networkz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.Env.NetworkManager.AllGateways())
}

func (s *DiscoveryServer) mcsz(w http.ResponseWriter, _ *http.Request) {
	svcs := sortMCSServices(s.Env.MCSServices())
	writeJSON(w, svcs)
}

func sortMCSServices(svcs []model.MCSServiceInfo) []model.MCSServiceInfo {
	sort.Slice(svcs, func(i, j int) bool {
		if strings.Compare(svcs[i].Cluster.String(), svcs[j].Cluster.String()) < 0 {
			return true
		}
		if strings.Compare(svcs[i].Namespace, svcs[j].Namespace) < 0 {
			return true
		}
		return strings.Compare(svcs[i].Name, svcs[j].Name) < 0
	})
	return svcs
}

func (s *DiscoveryServer) clusterz(w http.ResponseWriter, _ *http.Request) {
	if s.ListRemoteClusters == nil {
		w.WriteHeader(400)
		return
	}
	writeJSON(w, s.ListRemoteClusters())
}

// handlePushRequest handles a ?push=true query param and triggers a push.
// A boolean response is returned to indicate if the caller should continue
func (s *DiscoveryServer) handlePushRequest(w http.ResponseWriter, req *http.Request) bool {
	if err := req.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("Failed to parse request\n"))
		return true
	}
	if req.Form.Get("push") != "" {
		AdsPushAll(s)
		_, _ = fmt.Fprintf(w, "Pushed to %d servers\n", s.adsClientCount())
		return true
	}
	return false
}

// getDebugConnection fetches the Connection requested by proxyID
func (s *DiscoveryServer) getDebugConnection(req *http.Request) (string, *Connection) {
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		return proxyID, s.getProxyConnection(proxyID)
	}
	return "", nil
}

func (s *DiscoveryServer) errorHandler(w http.ResponseWriter, proxyID string, con *Connection) {
	if proxyID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("You must provide a proxyID in the query string\n"))
		return
	}
	if con == nil {
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance.\n"))
		return
	}
}

// jsonMarshalProto wraps a proto.Message so it can be marshaled with the standard encoding/json library
type jsonMarshalProto struct {
	proto.Message
}

func (p jsonMarshalProto) MarshalJSON() ([]byte, error) {
	return protomarshal.Marshal(p.Message)
}

// writeJSON writes a json payload, handling content type, marshaling, and errors
func writeJSON(w http.ResponseWriter, obj interface{}) {
	w.Header().Set("Content-Type", "application/json")
	b, err := config.ToJSON(obj)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	_, err = w.Write(b)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
	}
}

// handleHTTPError writes an error message to the response
func handleHTTPError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	_, _ = w.Write([]byte(err.Error()))
}
