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
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/http/pprof"
	"sort"

	"istio.io/istio/pkg/config/schema/collection"
	"istio.io/istio/pkg/kube/inject"

	"istio.io/istio/pilot/pkg/features"

	adminapi "github.com/envoyproxy/go-control-plane/envoy/admin/v3"
	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/ptypes"
	"github.com/golang/protobuf/ptypes/any"

	authn "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	networking_core "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	"istio.io/istio/pilot/pkg/networking/util"
	authn_alpha1 "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/host"
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

const (
	// configNameNotApplicable is used to represent the name of the authentication policy or
	// destination rule when they are not specified.
	configNameNotApplicable = "-"
)

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller, enableProfiling bool, webhook *inject.Webhook) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = NewMemServiceDiscovery(
		map[host.Name]*model.Service{ // mock.HelloService.Hostname: mock.HelloService,
		}, 2)
	s.MemRegistry.EDSUpdater = s
	s.MemRegistry.ClusterID = "v2-debug"

	sctl.AddRegistry(serviceregistry.Simple{
		ClusterID:        "v2-debug",
		ProviderID:       serviceregistry.Mock,
		ServiceDiscovery: s.MemRegistry,
		Controller:       s.MemRegistry.controller,
	})

	if enableProfiling {
		s.addDebugHandler(mux, "/debug/pprof/", "Displays pprof index", pprof.Index)
		s.addDebugHandler(mux, "/debug/pprof/cmdline", "The command line invocation of the current program", pprof.Cmdline)
		s.addDebugHandler(mux, "/debug/pprof/profile", "CPU profile", pprof.Profile)
		s.addDebugHandler(mux, "/debug/pprof/symbol", "Symbol looks up the program counters listed in the request", pprof.Symbol)
		s.addDebugHandler(mux, "/debug/pprof/trace", "A trace of execution of the current program.", pprof.Trace)
	}

	mux.HandleFunc("/debug", s.Debug)

	s.addDebugHandler(mux, "/debug/edsz", "Status and debug interface for EDS", s.edsz)
	s.addDebugHandler(mux, "/debug/adsz", "Status and debug interface for ADS", s.adsz)
	s.addDebugHandler(mux, "/debug/adsz?push=true", "Initiates push of the current state to all connected endpoints", s.adsz)
	s.addDebugHandler(mux, "/debug/cdsz", "Status and debug interface for CDS", s.cdsz)

	s.addDebugHandler(mux, "/debug/syncz", "Synchronization status of all Envoys connected to this Pilot instance", s.Syncz)
	s.addDebugHandler(mux, "/debug/config_distribution", "Version status of all Envoys connected to this Pilot instance", s.distributedVersions)

	s.addDebugHandler(mux, "/debug/registryz", "Debug support for registry", s.registryz)
	s.addDebugHandler(mux, "/debug/endpointz", "Debug support for endpoints", s.endpointz)
	s.addDebugHandler(mux, "/debug/endpointShardz", "Info about the endpoint shards", s.endpointShardz)
	s.addDebugHandler(mux, "/debug/configz", "Debug support for config", s.configz)

	s.addDebugHandler(mux, "/debug/authenticationz", "Dumps the authn tls-check info", s.Authenticationz)
	s.addDebugHandler(mux, "/debug/authorizationz", "Internal authorization policies", s.Authorizationz)
	s.addDebugHandler(mux, "/debug/config_dump", "ConfigDump in the form of the Envoy admin config dump API for passed in proxyID", s.ConfigDump)
	s.addDebugHandler(mux, "/debug/push_status", "Last PushContext Details", s.PushStatusHandler)

	s.addDebugHandler(mux, "/debug/inject", "Active inject template", s.InjectTemplateHandler(webhook))
}

func (s *DiscoveryServer) addDebugHandler(mux *http.ServeMux, path string, help string,
	handler func(http.ResponseWriter, *http.Request)) {
	s.debugHandlers[path] = help
	mux.HandleFunc(path, handler)
}

// SyncStatus is the synchronization status between Pilot and a given Envoy
type SyncStatus struct {
	ProxyID         string `json:"proxy,omitempty"`
	ProxyVersion    string `json:"proxy_version,omitempty"`
	IstioVersion    string `json:"istio_version,omitempty"`
	ClusterSent     string `json:"cluster_sent,omitempty"`
	ClusterAcked    string `json:"cluster_acked,omitempty"`
	ListenerSent    string `json:"listener_sent,omitempty"`
	ListenerAcked   string `json:"listener_acked,omitempty"`
	RouteSent       string `json:"route_sent,omitempty"`
	RouteAcked      string `json:"route_acked,omitempty"`
	EndpointSent    string `json:"endpoint_sent,omitempty"`
	EndpointAcked   string `json:"endpoint_acked,omitempty"`
	EndpointPercent int    `json:"endpoint_percent,omitempty"`
}

// Syncz dumps the synchronization status of all Envoys connected to this Pilot instance
func (s *DiscoveryServer) Syncz(w http.ResponseWriter, _ *http.Request) {
	syncz := make([]SyncStatus, 0)
	s.adsClientsMutex.RLock()
	for _, con := range s.adsClients {
		con.mu.RLock()
		if con.node != nil {
			syncz = append(syncz, SyncStatus{
				ProxyID:         con.node.ID,
				IstioVersion:    con.node.Metadata.IstioVersion,
				ClusterSent:     con.ClusterNonceSent,
				ClusterAcked:    con.ClusterNonceAcked,
				ListenerSent:    con.ListenerNonceSent,
				ListenerAcked:   con.ListenerNonceAcked,
				RouteSent:       con.RouteNonceSent,
				RouteAcked:      con.RouteNonceAcked,
				EndpointSent:    con.EndpointNonceSent,
				EndpointAcked:   con.EndpointNonceAcked,
				EndpointPercent: con.EndpointPercent,
			})
		}
		con.mu.RUnlock()
	}
	s.adsClientsMutex.RUnlock()
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

// Endpoint debugging
func (s *DiscoveryServer) endpointz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	brief := req.Form.Get("brief")
	if brief != "" {
		svc, _ := s.Env.ServiceDiscovery.Services()
		for _, ss := range svc {
			for _, p := range ss.Ports {
				all, err := s.Env.ServiceDiscovery.InstancesByPort(ss, p.Port, nil)
				if err != nil {
					return
				}
				for _, svc := range all {
					_, _ = fmt.Fprintf(w, "%s:%s %v %s:%d %v %s\n", ss.Hostname,
						p.Name, svc.Endpoint.Family, svc.Endpoint.Address, svc.Endpoint.EndpointPort, svc.Endpoint.Labels,
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
			all, err := s.Env.ServiceDiscovery.InstancesByPort(ss, p.Port, nil)
			if err != nil {
				return
			}
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

// SyncedVersions shows what resourceVersion of a given resource has been acked by Envoy.
type SyncedVersions struct {
	ProxyID         string `json:"proxy,omitempty"`
	ClusterVersion  string `json:"cluster_acked,omitempty"`
	ListenerVersion string `json:"listener_acked,omitempty"`
	RouteVersion    string `json:"route_acked,omitempty"`
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
		s.adsClientsMutex.RLock()
		for _, con := range s.adsClients {
			// wrap this in independent scope so that panic's don't bypass Unlock...
			con.mu.RLock()

			if con.node != nil && (proxyNamespace == "" || proxyNamespace == con.node.ConfigNamespace) {
				// TODO: handle skipped nodes
				results = append(results, SyncedVersions{
					ProxyID:         con.node.ID,
					ClusterVersion:  s.getResourceVersion(con.ClusterNonceAcked, resourceID, knownVersions),
					ListenerVersion: s.getResourceVersion(con.ListenerNonceAcked, resourceID, knownVersions),
					RouteVersion:    s.getResourceVersion(con.RouteNonceAcked, resourceID, knownVersions),
				})
			}
			con.mu.RUnlock()
		}
		s.adsClientsMutex.RUnlock()

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
		lookupResult, err := s.Env.IstioConfigStore.GetResourceAtVersion(configVersion, key)
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

// Config debugging.
func (s *DiscoveryServer) configz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, "\n[\n")

	var err error
	s.Env.IstioConfigStore.Schemas().ForEach(func(schema collection.Schema) bool {
		cfg, _ := s.Env.IstioConfigStore.List(schema.Resource().GroupVersionKind(), "")
		for _, c := range cfg {
			var b []byte
			b, err = json.MarshalIndent(c, "  ", "  ")
			if err != nil {
				// We're done.
				return true
			}
			_, _ = w.Write(b)
			_, _ = fmt.Fprint(w, ",\n")
		}
		return false
	})

	if err == nil {
		_, _ = fmt.Fprint(w, "\n{}]")
	}
}

// collectTLSSettingsForPort returns TLSSettings for the given port, key by subset name (the service-level settings
// should have key is an empty string). TLSSettings could be nil, indicate it was not set.
func collectTLSSettingsForPort(rule *networking.DestinationRule, port *model.Port) map[string]*networking.TLSSettings {
	if rule == nil {
		return map[string]*networking.TLSSettings{"": nil}
	}

	output := make(map[string]*networking.TLSSettings)
	output[""] = getTLSSettingsForTrafficPolicyAndPort(rule.TrafficPolicy, port)
	for _, subset := range rule.GetSubsets() {
		output[subset.GetName()] = getTLSSettingsForTrafficPolicyAndPort(subset.GetTrafficPolicy(), port)
	}

	return output
}

func getTLSSettingsForTrafficPolicyAndPort(trafficPolicy *networking.TrafficPolicy, port *model.Port) *networking.TLSSettings {
	if trafficPolicy == nil {
		return nil
	}
	_, _, _, tls := networking_core.SelectTrafficPolicyComponents(trafficPolicy, port)
	return tls
}

// AuthenticationDebug holds debug information for service authentication policy.
type AuthenticationDebug struct {
	Host                     string `json:"host"`
	Port                     int    `json:"port"`
	AuthenticationPolicyName string `json:"authentication_policy_name"`
	DestinationRuleName      string `json:"destination_rule_name"`
	ServerProtocol           string `json:"server_protocol"`
	ClientProtocol           string `json:"client_protocol"`
	TLSConflictStatus        string `json:"TLS_conflict_status"`
}

// Pretty to-string function for unit test log.
func (p *AuthenticationDebug) String() string {
	return fmt.Sprintf("{%s:%d, authn=%q, dr=%q, server=%q, client=%q, status=%q}", p.Host, p.Port, p.AuthenticationPolicyName,
		p.DestinationRuleName, p.ServerProtocol, p.ClientProtocol, p.TLSConflictStatus)
}

func configName(config *model.ConfigMeta) string {
	if config != nil {
		return fmt.Sprintf("%s/%s", config.Namespace, config.Name)
	}
	return configNameNotApplicable
}

// Authenticationz dumps the authn tls-check info.
// This handler lists what authentication policy is used for a service and destination rules to
// that service that a proxy instance received.
// Proxy ID (<pod>.<namespace> need to be provided  to correctly  determine which destination rules
// are visible.
func (s *DiscoveryServer) Authenticationz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		s.adsClientsMutex.RLock()
		defer s.adsClientsMutex.RUnlock()

		connections, ok := s.adsSidecarIDConnectionsMap[proxyID]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			// Need to dump an empty JSON array so istioctl can peacefully ignore.
			_, _ = fmt.Fprintf(w, "\n[\n]")
			return
		}

		var mostRecentProxy *model.Proxy
		mostRecent := ""
		for key := range connections {
			if mostRecent == "" || key > mostRecent {
				mostRecent = key
			}
		}
		mostRecentProxy = connections[mostRecent].node
		svc, _ := s.Env.ServiceDiscovery.Services()
		meshConfig := s.Env.Mesh()
		autoMTLSEnabled := meshConfig.GetEnableAutoMtls() != nil && meshConfig.GetEnableAutoMtls().Value
		info := make([]*AuthenticationDebug, 0)
		for _, ss := range svc {
			if ss.MeshExternal {
				// Skip external services
				continue
			}
			for _, p := range ss.Ports {
				authnPolicy, authnMeta := s.globalPushContext().AuthenticationPolicyForWorkload(ss, p)
				destConfig := s.globalPushContext().DestinationRule(mostRecentProxy, ss)
				info = append(info, AnalyzeMTLSSettings(autoMTLSEnabled, ss.Hostname, p, authnPolicy, authnMeta, destConfig)...)
			}
		}
		if b, err := json.MarshalIndent(info, "  ", "  "); err == nil {
			_, _ = w.Write(b)
		}
		return
	}

	w.WriteHeader(http.StatusBadRequest)
	_, _ = w.Write([]byte("You must provide a proxyID in the query string"))
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

// AnalyzeMTLSSettings returns mTLS compatibility status between client and server policies.
func AnalyzeMTLSSettings(autoMTLSEnabled bool, hostname host.Name, port *model.Port, authnPolicy *authn.Policy, authnMeta *model.ConfigMeta,
	destConfig *model.Config) []*AuthenticationDebug {
	authnPolicyName := configName(authnMeta)
	serverMTLSMode := authn_alpha1.GetMutualTLSMode(authnPolicy)

	baseDebugInfo := AuthenticationDebug{
		Port:                     port.Port,
		AuthenticationPolicyName: authnPolicyName,
		ServerProtocol:           serverMTLSMode.String(),
		ClientProtocol:           configNameNotApplicable,
	}

	var rule *networking.DestinationRule
	destinationRuleName := configNameNotApplicable

	if destConfig != nil {
		rule = destConfig.Spec.(*networking.DestinationRule)
		destinationRuleName = configName(&destConfig.ConfigMeta)
	}

	output := make([]*AuthenticationDebug, 0)

	clientTLSModes := collectTLSSettingsForPort(rule, port)
	var subsets []string
	for k := range clientTLSModes {
		subsets = append(subsets, k)
	}
	sort.Strings(subsets)

	for _, ss := range subsets {
		c := clientTLSModes[ss]
		info := baseDebugInfo
		info.DestinationRuleName = destinationRuleName
		if c != nil {
			info.ClientProtocol = c.GetMode().String()
		}
		if ss == "" {
			info.Host = string(hostname)
		} else {
			info.Host = fmt.Sprintf("%s|%s", hostname, ss)
		}
		info.TLSConflictStatus = EvaluateTLSState(autoMTLSEnabled, c, serverMTLSMode)

		output = append(output, &info)
	}
	return output
}

// EvaluateTLSState returns the conflict state (string) for the input client+server settings.
// The output string could be:
// - "AUTO": auto mTLS feature is enabled, client TLS (destination rule) is not set and pilot can auto detect client (m)TLS settings.
// - "OK": both client and server TLS settings are set correctly.
// - "CONFLICT": both client and server TLS settings are set, but could be incompatible.
func EvaluateTLSState(autoMTLSEnabled bool, clientMode *networking.TLSSettings, serverMode model.MutualTLSMode) string {
	const okState string = "OK"
	const conflictState string = "CONFLICT"
	const autoState string = "AUTO"

	if clientMode == nil {
		if autoMTLSEnabled {
			return autoState
		}
		// TLS settings was not set explicitly, pilot will try a setting that work well with the
		// destination authN policy. We could use the separate state value (e.g AUTO) in the future.
		return okState
	}

	if (serverMode == model.MTLSDisable && clientMode.GetMode() == networking.TLSSettings_DISABLE) ||
		(serverMode == model.MTLSStrict && clientMode.GetMode() == networking.TLSSettings_ISTIO_MUTUAL) ||
		(serverMode == model.MTLSPermissive &&
			(clientMode.GetMode() == networking.TLSSettings_ISTIO_MUTUAL || clientMode.GetMode() == networking.TLSSettings_DISABLE)) {
		return okState
	}

	return conflictState
}

// adsz implements a status and debug interface for ADS.
// It is mapped to /debug/adsz
func (s *DiscoveryServer) adsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	if req.Form.Get("push") != "" {
		AdsPushAll(s)
		s.adsClientsMutex.RLock()
		_, _ = fmt.Fprintf(w, "Pushed to %d servers", len(s.adsClients))
		s.adsClientsMutex.RUnlock()
		return
	}

	s.adsClientsMutex.RLock()
	defer s.adsClientsMutex.RUnlock()

	// Dirty json generation - because standard json is dirty (struct madness)
	// Unfortunately we must use the jsonbp to encode part of the json - I'm sure there are
	// better ways, but this is mainly for debugging.
	_, _ = fmt.Fprint(w, "[\n")
	comma := false
	for _, c := range s.adsClients {
		if comma {
			_, _ = fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		_, _ = fmt.Fprintf(w, "\n\n  {\"node\": \"%s\",\n \"addr\": \"%s\",\n \"connect\": \"%v\",\n \"listeners\":[\n", c.ConID, c.PeerAddr, c.Connect)
		printListeners(w, c)
		_, _ = fmt.Fprint(w, "],\n")
		_, _ = fmt.Fprintf(w, "\"RDSRoutes\":[\n")
		printRoutes(w, c)
		_, _ = fmt.Fprint(w, "],\n")
		_, _ = fmt.Fprintf(w, "\"clusters\":[\n")
		printClusters(w, c)
		_, _ = fmt.Fprint(w, "]}\n")
	}
	_, _ = fmt.Fprint(w, "]\n")
}

// ConfigDump returns information in the form of the Envoy admin API config dump for the specified proxy
// The dump will only contain dynamic listeners/clusters/routes and can be used to compare what an Envoy instance
// should look like according to Pilot vs what it currently does look like.
func (s *DiscoveryServer) ConfigDump(w http.ResponseWriter, req *http.Request) {
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		s.adsClientsMutex.RLock()
		defer s.adsClientsMutex.RUnlock()
		connections, ok := s.adsSidecarIDConnectionsMap[proxyID]
		if !ok || len(connections) == 0 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Proxy not connected to this Pilot instance"))
			return
		}

		jsonm := &jsonpb.Marshaler{Indent: "    "}
		mostRecent := ""
		for key := range connections {
			if mostRecent == "" || key > mostRecent {
				mostRecent = key
			}
		}
		dump, err := s.configDump(connections[mostRecent])
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
func (s *DiscoveryServer) configDump(conn *XdsConnection) (*adminapi.ConfigDump, error) {
	dynamicActiveClusters := make([]*adminapi.ClustersConfigDump_DynamicCluster, 0)
	clusters := s.generateRawClusters(conn.node, s.globalPushContext())

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
	listeners := s.generateRawListeners(conn, s.globalPushContext())
	for _, cs := range listeners {
		listener, err := ptypes.MarshalAny(cs)
		if err != nil {
			return nil, err
		}
		dynamicActiveListeners = append(dynamicActiveListeners, &adminapi.ListenersConfigDump_DynamicListener{
			ActiveState: &adminapi.ListenersConfigDump_DynamicListenerState{Listener: listener}})
	}
	listenersAny, err := util.MessageToAnyWithError(&adminapi.ListenersConfigDump{
		VersionInfo:      versionInfo(),
		DynamicListeners: dynamicActiveListeners,
	})
	if err != nil {
		return nil, err
	}

	routes := s.generateRawRoutes(conn, s.globalPushContext())
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

	bootstrapAny := util.MessageToAny(&adminapi.BootstrapConfigDump{})
	// The config dump must have all configs with connections specified in
	// https://www.envoyproxy.io/docs/envoy/latest/api-v2/admin/v2alpha/config_dump.proto
	configDump := &adminapi.ConfigDump{Configs: []*any.Any{bootstrapAny, clustersAny, listenersAny, routeConfigAny}}
	return configDump, nil
}

// InjectTemplateHandler dumps the injection template
// Replaces dumping the template at startup.
func (s *DiscoveryServer) InjectTemplateHandler(webhook *inject.Webhook) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, req *http.Request) {
		// TODO: we should split the inject template into smaller modules (separate one for dump core, etc),
		// and allow pods to select which patches will be selected. When this happen, this should return
		// all inject templates or take a param to select one.
		if webhook == nil {
			w.WriteHeader(404)
			return
		}

		_, _ = w.Write([]byte(webhook.Config.Template))
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
	w.WriteHeader(200)
}

// edsz implements a status and debug interface for EDS.
// It is mapped to /debug/edsz on the monitor port (15014).
func (s *DiscoveryServer) edsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	if req.Form.Get("push") != "" {
		AdsPushAll(s)
	}
	var con *XdsConnection
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		s.adsClientsMutex.RLock()
		defer s.adsClientsMutex.RUnlock()
		connections, ok := s.adsSidecarIDConnectionsMap[proxyID]
		// We can't guarantee the Pilot we are connected to has a connection to the proxy we requested
		// There isn't a great way around this, but for debugging purposes its suitable to have the caller retry.
		if !ok || len(connections) == 0 {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("Proxy not connected to this Pilot instance. It may be connected to another instance."))
			return
		}

		mostRecent := ""
		for key := range connections {
			if mostRecent == "" || key > mostRecent {
				mostRecent = key
			}
		}
		con = connections[mostRecent]
	}

	edsClusterMutex.RLock()
	defer edsClusterMutex.RUnlock()
	comma := false
	if con != nil {
		_, _ = fmt.Fprintln(w, "[")
		for _, clusterName := range con.Clusters {
			if comma {
				_, _ = fmt.Fprint(w, ",\n")
			} else {
				comma = true
			}
			cla := s.generateEndpoints(clusterName, con.node, s.globalPushContext(), nil)
			jsonm := &jsonpb.Marshaler{Indent: "  "}
			dbgString, _ := jsonm.MarshalToString(cla)
			if _, err := w.Write([]byte(dbgString)); err != nil {
				return
			}
		}
		_, _ = fmt.Fprintln(w, "]")
	} else if len(edsClusters) > 0 {
		_, _ = fmt.Fprintln(w, "[")
		for cluster := range edsClusters {
			if comma {
				_, _ = fmt.Fprint(w, ",\n")
			} else {
				comma = true
			}
			cla := s.loadAssignmentsForClusterLegacy(s.globalPushContext(), cluster)
			jsonm := &jsonpb.Marshaler{Indent: "  "}
			dbgString, _ := jsonm.MarshalToString(cla)
			if _, err := w.Write([]byte(dbgString)); err != nil {
				return
			}
		}
		_, _ = fmt.Fprintln(w, "]")
	} else {
		w.WriteHeader(404)
	}
}

// cdsz implements a status and debug interface for CDS.
// It is mapped to /debug/cdsz
func (s *DiscoveryServer) cdsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	s.adsClientsMutex.RLock()

	_, _ = fmt.Fprint(w, "[\n")
	comma := false
	for _, c := range s.adsClients {
		if comma {
			_, _ = fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		_, _ = fmt.Fprintf(w, "\n\n  {\"node\": \"%s\", \"addr\": \"%s\", \"connect\": \"%v\",\"Clusters\":[\n", c.ConID, c.PeerAddr, c.Connect)
		printClusters(w, c)
		_, _ = fmt.Fprint(w, "]}\n")
	}
	_, _ = fmt.Fprint(w, "]\n")

	s.adsClientsMutex.RUnlock()
}

func printListeners(w io.Writer, c *XdsConnection) {
	comma := false
	for _, ls := range c.LDSListeners {
		if ls == nil {
			adsLog.Errorf("INVALID LISTENER NIL")
			continue
		}
		if comma {
			_, _ = fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		jsonm := &jsonpb.Marshaler{Indent: "  "}
		dbgString, _ := jsonm.MarshalToString(ls)
		if _, err := w.Write([]byte(dbgString)); err != nil {
			return
		}
	}
}

func printClusters(w io.Writer, c *XdsConnection) {
	comma := false
	for _, cl := range c.CDSClusters {
		if cl == nil {
			adsLog.Errorf("INVALID Cluster NIL")
			continue
		}
		if comma {
			_, _ = fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		jsonm := &jsonpb.Marshaler{Indent: "  "}
		dbgString, _ := jsonm.MarshalToString(cl)
		if _, err := w.Write([]byte(dbgString)); err != nil {
			return
		}
	}
}

func printRoutes(w io.Writer, c *XdsConnection) {
	comma := false
	for _, rt := range c.RouteConfigs {
		if rt == nil {
			adsLog.Errorf("INVALID ROUTE CONFIG NIL")
			continue
		}
		if comma {
			_, _ = fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		jsonm := &jsonpb.Marshaler{Indent: "  "}
		dbgString, _ := jsonm.MarshalToString(rt)
		if _, err := w.Write([]byte(dbgString)); err != nil {
			return
		}
	}
}
