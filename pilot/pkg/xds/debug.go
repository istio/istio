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
	"net/netip"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	admin "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	wasm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/http/wasm/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	discoveryv3 "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	"google.golang.org/protobuf/proto"
	anypb "google.golang.org/protobuf/types/known/anypb"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/util/protoconv"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/resource"
	"istio.io/istio/pkg/config/xds"
	istiolog "istio.io/istio/pkg/log"
	"istio.io/istio/pkg/maps"
	"istio.io/istio/pkg/security"
	"istio.io/istio/pkg/slices"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/workloadapi"
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
<p style = "font-family:Arial,Helvetica,sans-serif;">
Note: Use <b>pretty</b> in query string (like <b>debug/configz?pretty</b>) to format the output.
</p>
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
	Labels       map[string]string   `json:"labels,omitempty"`
	Metadata     *model.NodeMetadata `json:"metadata,omitempty"`
	Locality     *core.Locality      `json:"locality,omitempty"`
	Watches      map[string][]string `json:"watches,omitempty"`
}

// AdsClients is collection of AdsClient connected to this Istiod.
type AdsClients struct {
	Total     int         `json:"totalClients"`
	Connected []AdsClient `json:"clients,omitempty"`
}

// SyncStatus is the synchronization status between Pilot and a given Envoy
type SyncStatus struct {
	ClusterID            string                    `json:"cluster_id,omitempty"`
	ProxyID              string                    `json:"proxy,omitempty"`
	ProxyType            model.NodeType            `json:"proxy_type,omitempty"`
	ProxyVersion         string                    `json:"proxy_version,omitempty"`
	IstioVersion         string                    `json:"istio_version,omitempty"`
	ClusterSent          string                    `json:"cluster_sent,omitempty"`
	ClusterAcked         string                    `json:"cluster_acked,omitempty"`
	ListenerSent         string                    `json:"listener_sent,omitempty"`
	ListenerAcked        string                    `json:"listener_acked,omitempty"`
	RouteSent            string                    `json:"route_sent,omitempty"`
	RouteAcked           string                    `json:"route_acked,omitempty"`
	EndpointSent         string                    `json:"endpoint_sent,omitempty"`
	EndpointAcked        string                    `json:"endpoint_acked,omitempty"`
	ExtensionConfigSent  string                    `json:"extensionconfig_sent,omitempty"`
	ExtensionConfigAcked string                    `json:"extensionconfig_acked,omitempty"`
	Resources            map[string]ResourceStatus `json:"resources,omitempty"`
}

type ResourceStatus struct {
	Sent      string    `json:"sent,omitempty"`
	Acked     string    `json:"acked,omitempty"`
	SentTime  time.Time `json:"sentTime,omitempty"`
	LastError string    `json:"lastError,omitempty"`
}

// SyncedVersions shows what resourceVersion of a given resource has been acked by Envoy.
type SyncedVersions struct {
	ProxyID         string `json:"proxy,omitempty"`
	ClusterVersion  string `json:"cluster_acked,omitempty"`
	ListenerVersion string `json:"listener_acked,omitempty"`
	RouteVersion    string `json:"route_acked,omitempty"`
	EndpointVersion string `json:"endpoint_acked,omitempty"`
}

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(
	mux *http.ServeMux,
	enableProfiling bool,
	fetchWebhook func() map[string]string,
) *http.ServeMux {
	internalMux := http.NewServeMux()
	s.AddDebugHandlers(mux, internalMux, enableProfiling, fetchWebhook)
	return internalMux
}

func (s *DiscoveryServer) AddDebugHandlers(mux, internalMux *http.ServeMux, enableProfiling bool, webhook func() map[string]string) {
	// Debug handlers on HTTP ports are added for backward compatibility.
	// They will be exposed on XDS-over-TLS in future releases.
	if !features.EnableDebugOnHTTP {
		return
	}

	if enableProfiling {
		runtime.SetMutexProfileFraction(features.MutexProfileFraction)
		runtime.SetBlockProfileRate(features.MutexProfileFraction)
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

	s.addDebugHandler(mux, internalMux, "/debug/ecdsz", "Status and debug interface for ECDS", s.typedConfigDumpHandler("ecds"))
	s.addDebugHandler(mux, internalMux, "/debug/edsz", "Status and debug interface for EDS", s.typedConfigDumpHandler("eds"))
	s.addDebugHandler(mux, internalMux, "/debug/ndsz", "Status and debug interface for NDS", s.typedConfigDumpHandler("nds"))
	s.addDebugHandler(mux, internalMux, "/debug/adsz", "Status and debug interface for ADS", s.adsz)
	s.addDebugHandler(mux, internalMux, "/debug/adsz?push=true", "Initiates push of the current state to all connected endpoints", s.adsz)

	s.addDebugHandler(mux, internalMux, "/debug/syncz", "Synchronization status of all Envoys connected to this Pilot instance", s.Syncz)

	s.addDebugHandler(mux, internalMux, "/debug/registryz", "Debug support for registry", s.registryz)
	s.addDebugHandler(mux, internalMux, "/debug/endpointz", "Obsolete, use endpointShardz", s.endpointShardz)
	s.addDebugHandler(mux, internalMux, "/debug/endpointShardz", "Info about the endpoint shards", s.endpointShardz)
	s.addDebugHandler(mux, internalMux, "/debug/cachez", "Info about the internal XDS caches", s.cachez)
	s.addDebugHandler(mux, internalMux, "/debug/cachez?sizes=true", "Info about the size of the internal XDS caches", s.cachez)
	s.addDebugHandler(mux, internalMux, "/debug/cachez?clear=true", "Clear the XDS caches", s.cachez)
	s.addDebugHandler(mux, internalMux, "/debug/configz", "Debug support for config", s.configz)
	s.addDebugHandler(mux, internalMux, "/debug/sidecarz", "Debug sidecar scope for a proxy", s.sidecarz)
	s.addDebugHandler(mux, internalMux, "/debug/resourcesz", "Debug support for watched resources", s.resourcez)
	s.addDebugHandler(mux, internalMux, "/debug/instancesz", "Debug support for service instances", s.instancesz)
	s.addDebugHandler(mux, internalMux, "/debug/ambientz", "Debug support for ambient", s.ambientz)
	s.addDebugHandler(mux, internalMux, "/debug/krtz", "Debug support for krt (internal state)", s.krtz)

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

	s.addDebugHandler(mux, internalMux, "/debug/list", "List all supported debug commands in json", s.list)
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
		authRequest := security.AuthContext{Request: req}
		for _, authn := range s.Authenticators {
			u, err := authn.Authenticate(authRequest)
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

	userIP, _ := netip.ParseAddr(ip)
	return userIP.IsLoopback()
}

// Syncz dumps the synchronization status of all Envoys connected to this Pilot instance
func (s *DiscoveryServer) Syncz(w http.ResponseWriter, req *http.Request) {
	namespace := req.URL.Query().Get("namespace")

	syncz := make([]SyncStatus, 0)
	for _, con := range s.SortedClients() {
		node := con.proxy
		if node != nil && (namespace == "" || node.GetNamespace() == namespace) {
			wrs := node.DeepCloneWatchedResources()
			res := make(map[string]ResourceStatus, len(wrs))
			for _, wr := range wrs {
				res[wr.TypeUrl] = ResourceStatus{
					Sent:      wr.NonceSent,
					Acked:     wr.NonceAcked,
					SentTime:  wr.LastSendTime,
					LastError: wr.LastError,
				}
			}
			syncz = append(syncz, SyncStatus{
				ProxyID:              node.ID,
				ProxyType:            node.Type,
				ClusterID:            node.GetClusterID().String(),
				IstioVersion:         node.GetIstioVersion(),
				ClusterSent:          node.NonceSent(v3.ClusterType),
				ClusterAcked:         node.NonceAcked(v3.ClusterType),
				ListenerSent:         node.NonceSent(v3.ListenerType),
				ListenerAcked:        node.NonceAcked(v3.ListenerType),
				RouteSent:            node.NonceSent(v3.RouteType),
				RouteAcked:           node.NonceAcked(v3.RouteType),
				EndpointSent:         node.NonceSent(v3.EndpointType),
				EndpointAcked:        node.NonceAcked(v3.EndpointType),
				ExtensionConfigSent:  node.NonceSent(v3.ExtensionConfigurationType),
				ExtensionConfigAcked: node.NonceAcked(v3.ExtensionConfigurationType),
				Resources:            res,
			})
		}
	}
	writeJSON(w, syncz, req)
}

// registryz providees debug support for registry - adding and listing model items.
// Can be combined with the push debug interface to reproduce changes.
func (s *DiscoveryServer) registryz(w http.ResponseWriter, req *http.Request) {
	all := s.Env.ServiceDiscovery.Services()
	writeJSON(w, all, req)
}

// Dumps info about the endpoint shards, tracked using the new direct interface.
// Legacy registry provides are synced to the new data structure as well, during
// the full push.
func (s *DiscoveryServer) endpointShardz(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, s.Env.EndpointIndex.Shardz(), req)
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
		raw := make(map[string]int, len(snapshot))
		totalSize := 0
		for _, resource := range snapshot {
			if resource == nil {
				continue
			}
			resourceType := resource.Resource.TypeUrl
			sz := len(resource.Resource.GetValue())
			raw[resourceType] += sz
			totalSize += sz
		}
		res := make(map[string]string, len(raw))
		for k, v := range raw {
			res[k] = util.ByteCount(v)
		}
		res["total"] = util.ByteCount(totalSize)
		writeJSON(w, res, req)
		return
	}
	snapshot := s.Cache.Snapshot()
	resources := make(map[string][]string, len(snapshot)) // Key is typeUrl and value is resource names.
	for _, resource := range snapshot {
		if resource == nil {
			continue
		}
		resourceType := resource.Resource.TypeUrl
		resources[resourceType] = append(resources[resourceType], resource.Name)
	}
	writeJSON(w, resources, req)
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
	if s.Env == nil || s.Env.ConfigStore == nil {
		return
	}
	s.Env.ConfigStore.Schemas().ForEach(func(schema resource.Schema) bool {
		cfg := s.Env.ConfigStore.List(schema.GroupVersionKind(), "")
		for _, c := range cfg {
			configs = append(configs, kubernetesConfig{c})
		}
		return false
	})
	writeJSON(w, configs, req)
}

// SidecarScope debugging
func (s *DiscoveryServer) sidecarz(w http.ResponseWriter, req *http.Request) {
	proxyID, con := s.getDebugConnection(req)
	if con == nil {
		s.errorHandler(w, proxyID, con)
		return
	}
	writeJSON(w, con.proxy.SidecarScope, req)
}

// Resource debugging.
func (s *DiscoveryServer) resourcez(w http.ResponseWriter, req *http.Request) {
	schemas := make([]config.GroupVersionKind, 0)

	if s.Env != nil && s.Env.ConfigStore != nil {
		s.Env.Schemas().ForEach(func(schema resource.Schema) bool {
			schemas = append(schemas, schema.GroupVersionKind())
			return false
		})
	}

	writeJSON(w, schemas, req)
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
	writeJSON(w, info, req)
}

// AuthorizationDebug holds debug information for authorization policy.
type TelemetryDebug struct {
	Telemetries *model.Telemetries `json:"telemetries"`
}

func (s *DiscoveryServer) telemetryz(w http.ResponseWriter, req *http.Request) {
	proxyID, con := s.getDebugConnection(req)
	if proxyID != "" && con == nil {
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance.\n"))
		return
	}
	if con == nil {
		info := TelemetryDebug{
			Telemetries: s.globalPushContext().Telemetry,
		}
		writeJSON(w, info, req)
		return
	}
	writeJSON(w, s.globalPushContext().Telemetry.Debug(con.proxy), req)
}

// connectionsHandler implements interface for displaying current connections.
// It is mapped to /debug/connections.
func (s *DiscoveryServer) connectionsHandler(w http.ResponseWriter, req *http.Request) {
	adsClients := &AdsClients{}
	connections := s.SortedClients()
	adsClients.Total = len(connections)

	for _, c := range connections {
		adsClient := AdsClient{
			ConnectionID: c.ID(),
			ConnectedAt:  c.ConnectedAt(),
			PeerAddress:  c.Peer(),
		}
		adsClients.Connected = append(adsClients.Connected, adsClient)
	}
	writeJSON(w, adsClients, req)
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
		connections = s.SortedClients()
	}

	adsClients := &AdsClients{}
	adsClients.Total = len(connections)
	for _, c := range connections {
		adsClient := AdsClient{
			ConnectionID: c.ID(),
			ConnectedAt:  c.ConnectedAt(),
			PeerAddress:  c.Peer(),
			Labels:       c.proxy.Labels,
			Metadata:     c.proxy.Metadata,
			Locality:     c.proxy.Locality,
			Watches:      map[string][]string{},
		}
		c.proxy.RLock()
		for k, wr := range c.proxy.WatchedResources {
			adsClient.Watches[k] = wr.ResourceNames.UnsortedList()
		}
		c.proxy.RUnlock()
		adsClients.Connected = append(adsClients.Connected, adsClient)
	}
	writeJSON(w, adsClients, req)
}

func (s *DiscoveryServer) typedConfigDumpHandler(typ string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		q.Set("types", typ)
		r.URL.RawQuery = q.Encode()
		s.ConfigDump(w, r)
	}
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
	if ts := s.getResourceTypes(req); len(ts) != 0 {
		resources := s.getConfigDumpByResourceType(con, nil, ts)
		configDump := &admin.ConfigDump{}
		for _, resource := range resources {
			for _, rr := range resource {
				configDump.Configs = append(configDump.Configs, rr.Resource)
			}
		}
		writeJSON(w, configDump, req)
		return
	}

	if con.proxy.IsZTunnel() {
		resources := s.getConfigDumpByResourceType(con, nil, []string{v3.AddressType})
		configDump := &admin.ConfigDump{}
		for _, resource := range resources {
			for _, rr := range resource {
				configDump.Configs = append(configDump.Configs, rr.Resource)
			}
		}
		writeJSON(w, configDump, req)
		return
	}

	includeEds := req.URL.Query().Has("include_eds")
	dump, err := s.connectionConfigDump(con, includeEds)
	if err != nil {
		handleHTTPError(w, err)
		return
	}
	writeJSON(w, dump, req)
}

func (s *DiscoveryServer) getResourceTypes(req *http.Request) []string {
	if shortTypes := req.URL.Query().Get("types"); shortTypes != "" {
		ts := strings.Split(shortTypes, ",")

		resourceTypes := sets.New[string]()
		for _, t := range ts {
			rType := v3.GetResourceType(t)
			resourceTypes.Insert(rType)
			// special case for AddressType, include WorkloadType as well
			// because they shared same short type name `WDS`
			if rType == v3.AddressType {
				resourceTypes.Insert(v3.WorkloadType)
			}
		}

		return resourceTypes.UnsortedList()
	}
	return nil
}

func (s *DiscoveryServer) getConfigDumpByResourceType(conn *Connection, req *model.PushRequest, ts []string) map[string][]*discoveryv3.Resource {
	dumps := make(map[string][]*discoveryv3.Resource)
	if req == nil {
		req = &model.PushRequest{Push: conn.proxy.LastPushContext, Start: time.Now(), Full: true}
	}

	for _, resourceType := range ts {
		w := conn.proxy.GetWatchedResource(resourceType)
		if w == nil {
			// Not watched, skip
			continue
		}
		gen := s.findGenerator(resourceType, conn)
		if gen == nil {
			// No generator found, skip
			continue
		}
		if resource, _, err := gen.Generate(conn.proxy, w, req); err == nil {
			for _, rr := range resource {
				switch resourceType {
				case v3.SecretType:
					// Secrets must be redacted
					secret := &tls.Secret{}
					if err := rr.Resource.UnmarshalTo(secret); err != nil {
						istiolog.Warnf("failed to unmarshal secret: %v", err)
						continue
					}
					if secret.GetTlsCertificate() != nil {
						// When utilizing XDS caching, the resource object is shared, so modifying this would modify the cached item
						// Make a clone
						rr = protomarshal.Clone(rr)
						secret.GetTlsCertificate().PrivateKey = &core.DataSource{
							Specifier: &core.DataSource_InlineBytes{
								InlineBytes: []byte("[redacted]"),
							},
						}
					}
					rr.Resource = protoconv.MessageToAny(secret)
					dumps[resourceType] = append(dumps[resourceType], rr)
				case v3.ExtensionConfigurationType:
					tce := &core.TypedExtensionConfig{}
					if err := rr.GetResource().UnmarshalTo(tce); err != nil {
						istiolog.Warnf("failed to unmarshal extension: %v", err)
						continue
					}

					switch tce.TypedConfig.TypeUrl {
					case xds.WasmHTTPFilterType:
						w := &wasm.Wasm{}
						if err := tce.TypedConfig.UnmarshalTo(w); err != nil {
							istiolog.Warnf("failed to unmarshal wasm filter: %v", err)
							continue
						}
						// Redact Wasm secret env variable.
						vmenvs := w.GetConfig().GetVmConfig().EnvironmentVariables
						if vmenvs != nil {
							if _, found := vmenvs.KeyValues[model.WasmSecretEnv]; found {
								vmenvs.KeyValues[model.WasmSecretEnv] = "<Redacted>"
							}
						}
						dumps[resourceType] = append(dumps[resourceType], &discoveryv3.Resource{
							Name:     w.Config.Name,
							Resource: protoconv.MessageToAny(w),
						})
					default:
						dumps[resourceType] = append(dumps[resourceType], rr)
					}
				default:
					dumps[resourceType] = append(dumps[resourceType], rr)
				}
			}
		} else {
			istiolog.Warnf("generate failed for request resource type (%v): %v", resourceType, err)
			continue
		}
	}

	return dumps
}

// connectionConfigDump converts the connection internal state into an Envoy Admin API config dump proto
// It is used in debugging to create a consistent object for comparison between Envoy and Pilot outputs
func (s *DiscoveryServer) connectionConfigDump(conn *Connection, includeEds bool) (*admin.ConfigDump, error) {
	req := &model.PushRequest{Push: conn.proxy.LastPushContext, Start: time.Now(), Full: true}
	version := req.Push.PushVersion

	dump := s.getConfigDumpByResourceType(conn, req, []string{
		v3.ClusterType,
		v3.ListenerType,
		v3.RouteType,
		v3.SecretType,
		v3.EndpointType,
		v3.ExtensionConfigurationType,
	})

	dynamicActiveClusters := make([]*admin.ClustersConfigDump_DynamicCluster, 0)
	for _, cluster := range dump[v3.ClusterType] {
		dynamicActiveClusters = append(dynamicActiveClusters, &admin.ClustersConfigDump_DynamicCluster{
			Cluster: cluster.Resource,
		})
	}
	clustersAny, err := protoconv.MessageToAnyWithError(&admin.ClustersConfigDump{
		VersionInfo:           version,
		DynamicActiveClusters: dynamicActiveClusters,
	})
	if err != nil {
		return nil, err
	}

	dynamicActiveListeners := make([]*admin.ListenersConfigDump_DynamicListener, 0)
	for _, listener := range dump[v3.ListenerType] {
		dynamicActiveListeners = append(dynamicActiveListeners, &admin.ListenersConfigDump_DynamicListener{
			Name: listener.Name,
			ActiveState: &admin.ListenersConfigDump_DynamicListenerState{
				Listener:    listener.Resource,
				VersionInfo: version,
			},
		})
	}
	listenersAny, err := protoconv.MessageToAnyWithError(&admin.ListenersConfigDump{
		VersionInfo:      version,
		DynamicListeners: dynamicActiveListeners,
	})
	if err != nil {
		return nil, err
	}

	dynamicRouteConfig := make([]*admin.RoutesConfigDump_DynamicRouteConfig, 0)
	for _, route := range dump[v3.RouteType] {
		dynamicRouteConfig = append(dynamicRouteConfig, &admin.RoutesConfigDump_DynamicRouteConfig{
			VersionInfo: version,
			RouteConfig: route.Resource,
		})
	}
	routesAny, err := protoconv.MessageToAnyWithError(&admin.RoutesConfigDump{
		DynamicRouteConfigs: dynamicRouteConfig,
	})
	if err != nil {
		return nil, err
	}

	dynamicSecretsConfig := make([]*admin.SecretsConfigDump_DynamicSecret, 0)
	for _, secret := range dump[v3.SecretType] {
		dynamicSecretsConfig = append(dynamicSecretsConfig, &admin.SecretsConfigDump_DynamicSecret{
			VersionInfo: version,
			Secret:      secret.Resource,
		})
	}
	secretsAny, err := protoconv.MessageToAnyWithError(&admin.SecretsConfigDump{
		DynamicActiveSecrets: dynamicSecretsConfig,
	})
	if err != nil {
		return nil, err
	}

	extensionsConfig := make([]*admin.EcdsConfigDump_EcdsFilterConfig, 0)
	for _, ext := range dump[v3.ExtensionConfigurationType] {
		extensionsConfig = append(extensionsConfig, &admin.EcdsConfigDump_EcdsFilterConfig{
			VersionInfo: version,
			EcdsFilter:  ext.Resource,
		})
	}
	extensionsAny, err := protoconv.MessageToAnyWithError(&admin.EcdsConfigDump{
		EcdsFilters: extensionsConfig,
	})
	if err != nil {
		return nil, err
	}

	var endpointsAny *anypb.Any
	// EDS is disabled by default for compatibility with Envoy config_dump interface
	if includeEds {
		endpointConfig := make([]*admin.EndpointsConfigDump_DynamicEndpointConfig, 0)
		for _, endpoint := range dump[v3.EndpointType] {
			endpointConfig = append(endpointConfig, &admin.EndpointsConfigDump_DynamicEndpointConfig{
				VersionInfo:    version,
				EndpointConfig: endpoint.Resource,
			})
		}
		endpointsAny, err = protoconv.MessageToAnyWithError(&admin.EndpointsConfigDump{
			DynamicEndpointConfigs: endpointConfig,
		})
		if err != nil {
			return nil, err
		}
	}

	bootstrapAny := protoconv.MessageToAny(&admin.BootstrapConfigDump{})
	scopedRoutesAny := protoconv.MessageToAny(&admin.ScopedRoutesConfigDump{})
	// The config dump must have all configs with connections specified in
	// https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	configs := []*anypb.Any{
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
		extensionsAny,
	)
	configDump := &admin.ConfigDump{
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

		writeJSON(w, webhook(), req)
	}
}

// meshHandler dumps the mesh config
func (s *DiscoveryServer) meshHandler(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, s.Env.Mesh(), req)
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
	NetworkGateways       []model.NetworkGateway
	UnresolvedGateways    []model.NetworkGateway
}

// pushContextHandler dumps the current PushContext
func (s *DiscoveryServer) pushContextHandler(w http.ResponseWriter, req *http.Request) {
	push := PushContextDebug{}
	pc := s.globalPushContext()
	if pc == nil {
		return
	}
	push.AuthorizationPolicies = pc.AuthzPolicies
	if pc.NetworkManager() != nil {
		push.NetworkGateways = pc.NetworkManager().AllGateways()
		push.UnresolvedGateways = pc.NetworkManager().Unresolved.AllGateways()
	}

	writeJSON(w, push, req)
}

// DebugEndpoints lists all the supported debug endpoints.
func (s *DiscoveryServer) DebugEndpoints() []string {
	return slices.Sort(maps.Keys(s.debugHandlers))
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

// list all the supported debug commands in json.
func (s *DiscoveryServer) list(w http.ResponseWriter, req *http.Request) {
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
	writeJSON(w, cmdNames, req)
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
		v.ResourceNames = v.ResourceNames.Copy()
		out.WatchedResources[k] = &v
	}
	return out
}

func (s *DiscoveryServer) getProxyConnection(proxyID string) *Connection {
	for _, con := range s.Clients() {
		if strings.Contains(con.ID(), proxyID) {
			out := *con
			out.proxy = cloneProxy(con.proxy)
			return &out
		}
	}

	return nil
}

func (s *DiscoveryServer) instancesz(w http.ResponseWriter, req *http.Request) {
	instances := map[string][]model.ServiceTarget{}
	for _, con := range s.Clients() {
		con.proxy.RLock()
		if con.proxy != nil {
			instances[con.proxy.ID] = con.proxy.ServiceTargets
		}
		con.proxy.RUnlock()
	}
	writeJSON(w, instances, req)
}

func (s *DiscoveryServer) ambientz(w http.ResponseWriter, req *http.Request) {
	addresses, _ := s.Env.ServiceDiscovery.AddressInformation(nil)
	res := struct {
		Workloads []jsonMarshalProto `json:"workloads"`
		Services  []jsonMarshalProto `json:"services"`
		Policies  []jsonMarshalProto `json:"policies"`
	}{}
	// WDS stores IPs as raw byte form. We want to view them as strings, so convert.
	// This doesn't quite work ideally, since json marshal will write as base64, but its better than nothing
	rewriteAddress := func(b []byte) []byte {
		ip, ok := netip.AddrFromSlice(b)
		if !ok {
			return b
		}
		return []byte(ip.String())
	}
	rewriteNetworkAddress := func(b *workloadapi.NetworkAddress) *workloadapi.NetworkAddress {
		nb := protomarshal.Clone(b)
		nb.Address = rewriteAddress(nb.Address)
		return nb
	}
	rewriteGatewayAddress := func(b *workloadapi.GatewayAddress) *workloadapi.GatewayAddress {
		if b == nil {
			return nil
		}
		nb := protomarshal.Clone(b)
		switch t := nb.Destination.(type) {
		case *workloadapi.GatewayAddress_Address:
			t.Address = rewriteNetworkAddress(t.Address)
		}
		return nb
	}
	for _, original := range addresses {
		addr := protomarshal.Clone(original.Address)
		switch addr := addr.Type.(type) {
		case *workloadapi.Address_Workload:
			w := addr.Workload
			w.Addresses = slices.Map(w.Addresses, rewriteAddress)
			w.Waypoint = rewriteGatewayAddress(w.Waypoint)
			w.NetworkGateway = rewriteGatewayAddress(w.NetworkGateway)
			res.Workloads = append(res.Workloads, jsonMarshalProto{w})
		case *workloadapi.Address_Service:
			s := addr.Service
			s.Addresses = slices.Map(s.Addresses, rewriteNetworkAddress)
			s.Waypoint = rewriteGatewayAddress(s.Waypoint)
			res.Services = append(res.Services, jsonMarshalProto{s})
		}
	}
	for _, policy := range s.Env.ServiceDiscovery.Policies(nil) {
		res.Policies = append(res.Policies, jsonMarshalProto{policy.Authorization})
	}
	writeJSON(w, res, req)
}

func (s *DiscoveryServer) krtz(w http.ResponseWriter, req *http.Request) {
	writeJSON(w, s.krtDebugger, req)
}

func (s *DiscoveryServer) networkz(w http.ResponseWriter, req *http.Request) {
	if s.Env == nil || s.Env.NetworkManager == nil {
		return
	}
	writeJSON(w, s.Env.NetworkManager.AllGateways(), req)
}

func (s *DiscoveryServer) mcsz(w http.ResponseWriter, req *http.Request) {
	svcs := sortMCSServices(s.Env.MCSServices())
	writeJSON(w, svcs, req)
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

func (s *DiscoveryServer) clusterz(w http.ResponseWriter, req *http.Request) {
	if s.ListRemoteClusters == nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	writeJSON(w, s.ListRemoteClusters(), req)
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
func writeJSON(w http.ResponseWriter, obj any, req *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var b []byte
	var err error
	if req.URL.Query().Has("pretty") {
		b, err = config.ToPrettyJSON(obj)
	} else {
		b, err = config.ToJSON(obj)
	}
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
