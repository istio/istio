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
	"net/http"
	"net/http/pprof"
	"sort"
	"strings"
	"time"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	tls "github.com/envoyproxy/go-control-plane/envoy/extensions/transport_sockets/tls/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"

	"istio.io/istio/pilot/pkg/config/kube/crd"
	"istio.io/istio/pilot/pkg/features"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pilot/pkg/serviceregistry/memory"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/pkg/log"
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
	Watches      map[string][]string `json:"watches"`
}

// AdsClients is collection of AdsClient connected to this Istiod.
type AdsClients struct {
	Total     int         `json:"totalClients"`
	Connected []AdsClient `json:"clients"`
}

// SyncStatus is the synchronization status between Pilot and a given Envoy
type SyncStatus struct {
	ProxyID       string `json:"proxy,omitempty"`
	ProxyVersion  string `json:"proxy_version,omitempty"`
	IstioVersion  string `json:"istio_version,omitempty"`
	ClusterSent   string `json:"cluster_sent,omitempty"`
	ClusterAcked  string `json:"cluster_acked,omitempty"`
	ListenerSent  string `json:"listener_sent,omitempty"`
	ListenerAcked string `json:"listener_acked,omitempty"`
	RouteSent     string `json:"route_sent,omitempty"`
	RouteAcked    string `json:"route_acked,omitempty"`
	EndpointSent  string `json:"endpoint_sent,omitempty"`
	EndpointAcked string `json:"endpoint_acked,omitempty"`
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
	fetchWebhook func() map[string]string) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = memory.NewServiceDiscovery(nil)
	s.MemRegistry.EDSUpdater = s
	s.MemRegistry.ClusterID = "v2-debug"

	sctl.AddRegistry(serviceregistry.Simple{
		ClusterID:        "v2-debug",
		ProviderID:       serviceregistry.Mock,
		ServiceDiscovery: s.MemRegistry,
		Controller:       s.MemRegistry.Controller,
	})
	s.AddDebugHandlers(mux, enableProfiling, fetchWebhook)
	debugGen, ok := (s.Generators[TypeDebug]).(*DebugGen)
	if ok {
		debugGen.DebugMux = mux
	}
}

func (s *DiscoveryServer) AddDebugHandlers(mux *http.ServeMux, enableProfiling bool, webhook func() map[string]string) {
	// Debug handlers on HTTP ports are added for backward compatibility.
	// They will be exposed on XDS-over-TLS in future releases.
	if !features.EnableDebugOnHTTP {
		return
	}

	if enableProfiling {
		s.addDebugHandler(mux, "/debug/pprof/", "Displays pprof index", pprof.Index)
		s.addDebugHandler(mux, "/debug/pprof/cmdline", "The command line invocation of the current program", pprof.Cmdline)
		s.addDebugHandler(mux, "/debug/pprof/profile", "CPU profile", pprof.Profile)
		s.addDebugHandler(mux, "/debug/pprof/symbol", "Symbol looks up the program counters listed in the request", pprof.Symbol)
		s.addDebugHandler(mux, "/debug/pprof/trace", "A trace of execution of the current program.", pprof.Trace)
	}

	mux.HandleFunc("/debug", s.Debug)

	if features.EnableUnsafeAdminEndpoints {
		s.addDebugHandler(mux, "/debug/force_disconnect", "Disconnects a proxy from this Pilot", s.ForceDisconnect)
	}

	s.addDebugHandler(mux, "/debug/edsz", "Status and debug interface for EDS", s.Edsz)
	s.addDebugHandler(mux, "/debug/ndsz", "Status and debug interface for NDS", s.Ndsz)
	s.addDebugHandler(mux, "/debug/adsz", "Status and debug interface for ADS", s.adsz)
	s.addDebugHandler(mux, "/debug/adsz?push=true", "Initiates push of the current state to all connected endpoints", s.adsz)

	s.addDebugHandler(mux, "/debug/syncz", "Synchronization status of all Envoys connected to this Pilot instance", s.Syncz)
	s.addDebugHandler(mux, "/debug/config_distribution", "Version status of all Envoys connected to this Pilot instance", s.distributedVersions)

	s.addDebugHandler(mux, "/debug/registryz", "Debug support for registry", s.registryz)
	s.addDebugHandler(mux, "/debug/endpointz", "Debug support for endpoints", s.endpointz)
	s.addDebugHandler(mux, "/debug/endpointShardz", "Info about the endpoint shards", s.endpointShardz)
	s.addDebugHandler(mux, "/debug/cachez", "Info about the internal XDS caches", s.cachez)
	s.addDebugHandler(mux, "/debug/configz", "Debug support for config", s.configz)
	s.addDebugHandler(mux, "/debug/sidecarz", "Debug sidecar scope for a proxy", s.sidecarz)
	s.addDebugHandler(mux, "/debug/resourcesz", "Debug support for watched resources", s.resourcez)
	s.addDebugHandler(mux, "/debug/instancesz", "Debug support for service instances", s.instancesz)

	s.addDebugHandler(mux, "/debug/authorizationz", "Internal authorization policies", s.Authorizationz)
	s.addDebugHandler(mux, "/debug/config_dump", "ConfigDump in the form of the Envoy admin config dump API for passed in proxyID", s.ConfigDump)
	s.addDebugHandler(mux, "/debug/push_status", "Last PushContext Details", s.PushStatusHandler)
	s.addDebugHandler(mux, "/debug/pushcontext", "Debug support for current push context", s.PushContextHandler)
	s.addDebugHandler(mux, "/debug/connections", "Info about the connected XDS clients", s.ConnectionsHandler)

	s.addDebugHandler(mux, "/debug/inject", "Active inject template", s.InjectTemplateHandler(webhook))
	s.addDebugHandler(mux, "/debug/mesh", "Active mesh config", s.MeshHandler)
}

func (s *DiscoveryServer) addDebugHandler(mux *http.ServeMux, path string, help string,
	handler func(http.ResponseWriter, *http.Request)) {
	s.debugHandlers[path] = help
	mux.HandleFunc(path, handler)
}

// Syncz dumps the synchronization status of all Envoys connected to this Pilot instance
func (s *DiscoveryServer) Syncz(w http.ResponseWriter, _ *http.Request) {
	syncz := make([]SyncStatus, 0)
	for _, con := range s.Clients() {
		node := con.proxy
		if node != nil {
			syncz = append(syncz, SyncStatus{
				ProxyID:       node.ID,
				IstioVersion:  node.Metadata.IstioVersion,
				ClusterSent:   con.NonceSent(v3.ClusterType),
				ClusterAcked:  con.NonceAcked(v3.ClusterType),
				ListenerSent:  con.NonceSent(v3.ListenerType),
				ListenerAcked: con.NonceAcked(v3.ListenerType),
				RouteSent:     con.NonceSent(v3.RouteType),
				RouteAcked:    con.NonceAcked(v3.RouteType),
				EndpointSent:  con.NonceSent(v3.EndpointType),
				EndpointAcked: con.NonceAcked(v3.EndpointType),
			})
		}
	}
	out, err := json.MarshalIndent(&syncz, "", "    ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "unable to marshal syncz information: %v", err)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(out)
}

// registryz providees debug support for registry - adding and listing model items.
// Can be combined with the push debug interface to reproduce changes.
func (s *DiscoveryServer) registryz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	all, err := s.Env.ServiceDiscovery.Services()
	if err != nil {
		return
	}
	_, _ = fmt.Fprintln(w, "[")
	for _, svc := range all {
		b, err := json.MarshalIndent(svc, "", "  ")
		if err != nil {
			return
		}
		_, _ = w.Write(b)
		_, _ = fmt.Fprintln(w, ",")
	}
	_, _ = fmt.Fprintln(w, "{}]")
}

// Dumps info about the endpoint shards, tracked using the new direct interface.
// Legacy registry provides are synced to the new data structure as well, during
// the full push.
func (s *DiscoveryServer) endpointShardz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	s.mutex.RLock()
	out, _ := json.MarshalIndent(s.EndpointShardsByService, " ", " ")
	s.mutex.RUnlock()
	_, _ = w.Write(out)
}

func (s *DiscoveryServer) cachez(w http.ResponseWriter, req *http.Request) {
	keys := s.Cache.Keys()
	sort.Strings(keys)
	bytes, err := json.Marshal(keys)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "unable to marshal syncedVersion information: %v", err)
		return
	}
	_, _ = w.Write(bytes)
}

// Endpoint debugging
func (s *DiscoveryServer) endpointz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	brief := req.Form.Get("brief")
	if brief != "" {
		svc, _ := s.Env.ServiceDiscovery.Services()
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

	svc, _ := s.Env.ServiceDiscovery.Services()
	_, _ = fmt.Fprint(w, "[\n")
	for _, ss := range svc {
		for _, p := range ss.Ports {
			all := s.Env.ServiceDiscovery.InstancesByPort(ss, p.Port, nil)
			_, _ = fmt.Fprintf(w, "\n{\"svc\": \"%s:%s\", \"ep\": [\n", ss.Hostname, p.Name)
			for _, svc := range all {
				b, err := json.MarshalIndent(svc, "  ", "  ")
				if err != nil {
					return
				}
				_, _ = w.Write(b)
				_, _ = fmt.Fprint(w, ",\n")
			}
			_, _ = fmt.Fprint(w, "\n{}]},")
		}
	}
	_, _ = fmt.Fprint(w, "\n{}]\n")
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
					ClusterVersion: s.getResourceVersion(s.StatusReporter.QueryLastNonce(con.ConID, v3.ClusterType),
						resourceID, knownVersions),
					ListenerVersion: s.getResourceVersion(s.StatusReporter.QueryLastNonce(con.ConID, v3.ListenerType),
						resourceID, knownVersions),
					RouteVersion: s.getResourceVersion(s.StatusReporter.QueryLastNonce(con.ConID, v3.RouteType),
						resourceID, knownVersions),
				})
			}
			con.proxy.RUnlock()
		}

		out, err := json.MarshalIndent(&results, "", "    ")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = fmt.Fprintf(w, "unable to marshal syncedVersion information: %v", err)
			return
		}
		w.Header().Add("Content-Type", "application/json")
		_, _ = w.Write(out)
	} else {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = fmt.Fprintf(w, "querystring parameter 'resource' is required")
	}
}

// The Config Version is only used as the nonce prefix, but we can reconstruct it because is is a
// b64 encoding of a 64 bit array, which will always be 12 chars in length.
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
			adsLog.Errorf("Unable to retrieve resource %s at version %s: %v", key, configVersion, err)
			lookupResult = ""
		}
		// update the cache even on an error, because errors will not resolve themselves, and we don't want to
		// repeat the same error for many s.adsClients.
		cache[configVersion] = lookupResult
		return lookupResult
	}
	return result
}

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
	configs := []kubernetesConfig{}
	s.Env.IstioConfigStore.Schemas().ForEach(func(schema collection.Schema) bool {
		cfg, _ := s.Env.IstioConfigStore.List(schema.Resource().GroupVersionKind(), "")
		for _, c := range cfg {
			configs = append(configs, kubernetesConfig{c})
		}
		return false
	})
	b, err := json.MarshalIndent(configs, "  ", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	w.Header().Add("Content-Type", "application/json")
	_, _ = w.Write(b)
}

// SidecarScope debugging
func (s *DiscoveryServer) sidecarz(w http.ResponseWriter, req *http.Request) {
	proxyID := req.URL.Query().Get("proxyID")
	if proxyID == "" {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("You must provide a proxyID in the query string"))
		return
	}

	con := s.getProxyConnection(proxyID)
	if con == nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("Proxy not connected to this Pilot instance"))
		return
	}
	by, err := json.MarshalIndent(con.proxy.SidecarScope, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(err.Error()))
		return
	}
	_, _ = w.Write(by)
}

// Resource debugging.
func (s *DiscoveryServer) resourcez(w http.ResponseWriter, _ *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	schemas := []config.GroupVersionKind{}
	s.Env.Schemas().ForEach(func(schema collection.Schema) bool {
		schemas = append(schemas, schema.Resource().GroupVersionKind())
		return false
	})

	if b, err := json.MarshalIndent(schemas, "", "  "); err == nil {
		_, _ = w.Write(b)
	}
}

// AuthorizationDebug holds debug information for authorization policy.
type AuthorizationDebug struct {
	AuthorizationPolicies *model.AuthorizationPolicies `json:"authorization_policies"`
}

// Authorizationz dumps the internal authorization policies.
func (s *DiscoveryServer) Authorizationz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	info := AuthorizationDebug{
		AuthorizationPolicies: s.globalPushContext().AuthzPolicies,
	}
	if b, err := json.MarshalIndent(info, "  ", "  "); err == nil {
		_, _ = w.Write(b)
	}
}

// ConnectionsHandler implements interface for displaying current connections.
// It is mapped to /debug/connections.
func (s *DiscoveryServer) ConnectionsHandler(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	adsClients := &AdsClients{}
	connections := s.Clients()
	adsClients.Total = len(connections)

	for _, c := range connections {
		adsClient := AdsClient{
			ConnectionID: c.ConID,
			ConnectedAt:  c.Connect,
			PeerAddress:  c.PeerAddr,
		}
		adsClients.Connected = append(adsClients.Connected, adsClient)
	}
	if b, err := json.MarshalIndent(adsClients, "  ", "  "); err == nil {
		_, _ = w.Write(b)
	}
}

// adsz implements a status and debug interface for ADS.
// It is mapped to /debug/adsz
func (s *DiscoveryServer) adsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	if req.Form.Get("push") != "" {
		AdsPushAll(s)
		_, _ = fmt.Fprintf(w, "Pushed to %d servers", s.adsClientCount())
		return
	}

	adsClients := &AdsClients{}
	connections := s.Clients()
	adsClients.Total = len(connections)
	for _, c := range s.Clients() {
		adsClient := AdsClient{
			ConnectionID: c.ConID,
			ConnectedAt:  c.Connect,
			PeerAddress:  c.PeerAddr,
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
	if b, err := json.MarshalIndent(adsClients, "  ", "  "); err == nil {
		_, _ = w.Write(b)
	}
}

// ConfigDump returns information in the form of the Envoy admin API config dump for the specified proxy
// The dump will only contain dynamic listeners/clusters/routes and can be used to compare what an Envoy instance
// should look like according to Pilot vs what it currently does look like.
func (s *DiscoveryServer) ConfigDump(w http.ResponseWriter, req *http.Request) {
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		con := s.getProxyConnection(proxyID)
		if con == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Proxy not connected to this Pilot instance"))
			return
		}

		jsonm := &jsonpb.Marshaler{Indent: "    "}
		dump, err := s.configDump(con)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		if err := jsonm.Marshal(w, dump); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
			return
		}
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte("You must provide a proxyID in the query string"))
}

// configDump converts the connection internal state into an Envoy Admin API config dump proto
// It is used in debugging to create a consistent object for comparison between Envoy and Pilot outputs
func (s *DiscoveryServer) configDump(conn *Connection) (*adminapi.ConfigDump, error) {
	dynamicActiveClusters := make([]*adminapi.ClustersConfigDump_DynamicCluster, 0)
	clusters := s.ConfigGenerator.BuildClusters(conn.proxy, s.globalPushContext())

	for _, cs := range clusters {
		cluster, err := ptypes.MarshalAny(cs)
		if err != nil {
			return nil, err
		}
		dynamicActiveClusters = append(dynamicActiveClusters, &adminapi.ClustersConfigDump_DynamicCluster{Cluster: cluster})
	}
	clustersAny, err := util.MessageToAnyWithError(&adminapi.ClustersConfigDump{
		VersionInfo:           versionInfo(),
		DynamicActiveClusters: dynamicActiveClusters,
	})
	if err != nil {
		return nil, err
	}

	dynamicActiveListeners := make([]*adminapi.ListenersConfigDump_DynamicListener, 0)
	listeners := s.ConfigGenerator.BuildListeners(conn.proxy, s.globalPushContext())
	for _, cs := range listeners {
		listener, err := ptypes.MarshalAny(cs)
		if err != nil {
			return nil, err
		}
		dynamicActiveListeners = append(dynamicActiveListeners, &adminapi.ListenersConfigDump_DynamicListener{
			Name:        cs.Name,
			ActiveState: &adminapi.ListenersConfigDump_DynamicListenerState{Listener: listener},
		})
	}
	listenersAny, err := util.MessageToAnyWithError(&adminapi.ListenersConfigDump{
		VersionInfo:      versionInfo(),
		DynamicListeners: dynamicActiveListeners,
	})
	if err != nil {
		return nil, err
	}

	routes := s.ConfigGenerator.BuildHTTPRoutes(conn.proxy, s.globalPushContext(), conn.Routes())
	routeConfigAny := util.MessageToAny(&adminapi.RoutesConfigDump{})
	if len(routes) > 0 {
		dynamicRouteConfig := make([]*adminapi.RoutesConfigDump_DynamicRouteConfig, 0)
		for _, rs := range routes {
			route, err := ptypes.MarshalAny(rs)
			if err != nil {
				return nil, err
			}
			dynamicRouteConfig = append(dynamicRouteConfig, &adminapi.RoutesConfigDump_DynamicRouteConfig{RouteConfig: route})
		}
		routeConfigAny, err = util.MessageToAnyWithError(&adminapi.RoutesConfigDump{DynamicRouteConfigs: dynamicRouteConfig})
		if err != nil {
			return nil, err
		}
	}

	secretsDump := &adminapi.SecretsConfigDump{}
	if s.Generators[v3.SecretType] != nil {
		secrets, _ := s.Generators[v3.SecretType].Generate(conn.proxy, s.globalPushContext(), conn.Watched(v3.SecretType), nil)
		if len(secrets) > 0 {
			for _, secretAny := range secrets {
				secret := &tls.Secret{}
				if err := ptypes.UnmarshalAny(secretAny, secret); err != nil {
					log.Warnf("failed to unmarshal secret: %v", err)
				}
				if secret.GetTlsCertificate() != nil {
					secret.GetTlsCertificate().PrivateKey = &core.DataSource{
						Specifier: &core.DataSource_InlineBytes{
							InlineBytes: []byte("[redacted]"),
						},
					}
				}
				secretsDump.DynamicActiveSecrets = append(secretsDump.DynamicActiveSecrets, &adminapi.SecretsConfigDump_DynamicSecret{
					Name:   secret.Name,
					Secret: util.MessageToAny(secret),
				})
			}
		}
	}

	bootstrapAny := util.MessageToAny(&adminapi.BootstrapConfigDump{})
	scopedRoutesAny := util.MessageToAny(&adminapi.ScopedRoutesConfigDump{})
	// The config dump must have all configs with connections specified in
	// https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	configDump := &adminapi.ConfigDump{
		Configs: []*any.Any{
			bootstrapAny,
			clustersAny, listenersAny,
			scopedRoutesAny,
			routeConfigAny,
			util.MessageToAny(secretsDump),
		},
	}
	return configDump, nil
}

// InjectTemplateHandler dumps the injection template
// Replaces dumping the template at startup.
func (s *DiscoveryServer) InjectTemplateHandler(webhook func() map[string]string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		// TODO: we should split the inject template into smaller modules (separate one for dump core, etc),
		// and allow pods to select which patches will be selected. When this happen, this should return
		// all inject templates or take a param to select one.
		if webhook == nil {
			w.WriteHeader(404)
			return
		}

		by, err := json.MarshalIndent(webhook(), "", "  ")
		if err != nil {
			w.WriteHeader(503)
			return
		}

		_, _ = w.Write(by)
	}
}

// MeshHandler dumps the mesh config
func (s *DiscoveryServer) MeshHandler(w http.ResponseWriter, r *http.Request) {
	if err := (&jsonpb.Marshaler{Indent: "  "}).Marshal(w, s.Env.Mesh()); err != nil {
		w.WriteHeader(500)
	}
}

// PushStatusHandler dumps the last PushContext
func (s *DiscoveryServer) PushStatusHandler(w http.ResponseWriter, req *http.Request) {
	if model.LastPushStatus == nil {
		return
	}
	out, err := model.LastPushStatus.StatusJSON()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "unable to marshal push information: %v", err)
		return
	}
	w.Header().Add("Content-Type", "application/json")

	_, _ = w.Write(out)
}

// PushContextDebug holds debug information for push context.
type PushContextDebug struct {
	AuthorizationPolicies *model.AuthorizationPolicies
	NetworkGateways       map[string][]*model.Gateway
}

// PushContextHandler dumps the current PushContext
func (s *DiscoveryServer) PushContextHandler(w http.ResponseWriter, req *http.Request) {
	push := PushContextDebug{
		AuthorizationPolicies: s.globalPushContext().AuthzPolicies,
		NetworkGateways:       s.globalPushContext().NetworkGateways(),
	}

	out, err := json.MarshalIndent(push, "", "  ")
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "unable to marshal push context information: %v", err)
		return
	}
	w.Header().Add("Content-Type", "application/json")

	_, _ = w.Write(out)
}

// lists all the supported debug endpoints.
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
		adsLog.Errorf("Error in rendering index template %v", err)
		w.WriteHeader(500)
	}
}

// Ndsz implements a status and debug interface for NDS.
// It is mapped to /debug/Ndsz on the monitor port (15014).
func (s *DiscoveryServer) Ndsz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")

	if req.Form.Get("push") != "" {
		AdsPushAll(s)
	}
	var con *Connection
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		con = s.getProxyConnection(proxyID)
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		if con == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance."))
			return
		}
	} else {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("You must provide a proxyID in the query string"))
		return
	}
	if _, err := w.Write([]byte("[")); err != nil {
		return
	}

	if s.Generators[v3.NameTableType] != nil {
		nds, _ := s.Generators[v3.NameTableType].Generate(con.proxy, s.globalPushContext(), nil, nil)
		if len(nds) == 0 {
			return
		}
		jsonm := &jsonpb.Marshaler{Indent: "  "}
		_ = jsonm.Marshal(w, nds[0])
	}
}

// Edsz implements a status and debug interface for EDS.
// It is mapped to /debug/edsz on the monitor port (15014).
func (s *DiscoveryServer) Edsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	if req.Form.Get("push") != "" {
		AdsPushAll(s)
	}
	var con *Connection
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		con = s.getProxyConnection(proxyID)
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		if con == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance."))
			return
		}
	} else {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("You must provide a proxyID in the query string"))
		return
	}

	comma := false
	_, _ = fmt.Fprintln(w, "[")
	for _, clusterName := range con.Clusters() {
		if comma {
			_, _ = fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		cla := s.generateEndpoints(NewEndpointBuilder(clusterName, con.proxy, s.globalPushContext()))
		jsonm := &jsonpb.Marshaler{Indent: "  "}
		dbgString, _ := jsonm.MarshalToString(cla)
		if _, err := w.Write([]byte(dbgString)); err != nil {
			return
		}
	}
	_, _ = fmt.Fprintln(w, "]")
}

func (s *DiscoveryServer) ForceDisconnect(w http.ResponseWriter, req *http.Request) {
	var con *Connection
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		con = s.getProxyConnection(proxyID)
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		if con == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance."))
			return
		}
	} else {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("You must provide a proxyID in the query string"))
		return
	}
	con.Stop()
	_, _ = w.Write([]byte("OK"))
}

func (s *DiscoveryServer) getProxyConnection(proxyID string) *Connection {
	for _, con := range s.Clients() {
		if strings.Contains(con.ConID, proxyID) {
			return con
		}
	}

	return nil
}

func (s *DiscoveryServer) instancesz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	instances := map[string][]*model.ServiceInstance{}
	for _, con := range s.Clients() {
		con.proxy.RLock()
		if con.proxy != nil {
			instances[con.proxy.ID] = con.proxy.ServiceInstances
		}
		con.proxy.RUnlock()
	}
	by, _ := json.MarshalIndent(instances, "", "  ")

	_, _ = w.Write(by)
}
