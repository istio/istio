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
	"io"
	"net/http"
	"sort"

	"github.com/golang/protobuf/jsonpb"

	authn "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"

	"istio.io/istio/pilot/pkg/model"
	networking_core "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	authn_alpha1 "istio.io/istio/pilot/pkg/security/authn/v1alpha1"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
	"istio.io/istio/pkg/config/host"
)

const (
	// configNameNotApplicable is used to represent the name of the authentication policy or
	// destination rule when they are not specified.
	configNameNotApplicable = "-"

	// configNameUnknown is used to represent the name of the authentication policy when it is specified,
	// but the information is not available to show. This is temporary, until we fix the push context to carry
	// authN policy name.
	configNameUnknown = "???"
)

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = NewMemServiceDiscovery(
		map[host.Name]*model.Service{ // mock.HelloService.Hostname: mock.HelloService,
		}, 2)
	s.MemRegistry.EDSUpdater = s
	s.MemRegistry.ClusterID = "v2-debug"

	sctl.AddRegistry(aggregate.Registry{
		ClusterID:        "v2-debug",
		Name:             serviceregistry.ServiceRegistry("memAdapter"),
		ServiceDiscovery: s.MemRegistry,
		Controller:       s.MemRegistry.controller,
	})

	mux.HandleFunc("/ready", s.ready)

	mux.HandleFunc("/debug/edsz", s.edsz)
	mux.HandleFunc("/debug/adsz", s.adsz)
	mux.HandleFunc("/debug/cdsz", cdsz)
	mux.HandleFunc("/debug/syncz", Syncz)

	mux.HandleFunc("/debug/registryz", s.registryz)
	mux.HandleFunc("/debug/endpointz", s.endpointz)
	mux.HandleFunc("/debug/endpointShardz", s.endpointShardz)
	mux.HandleFunc("/debug/configz", s.configz)

	mux.HandleFunc("/debug/authenticationz", s.Authenticationz)
	mux.HandleFunc("/debug/config_dump", s.ConfigDump)
	mux.HandleFunc("/debug/push_status", s.PushStatusHandler)
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
func Syncz(w http.ResponseWriter, _ *http.Request) {
	syncz := make([]SyncStatus, 0)
	adsClientsMutex.RLock()
	for _, con := range adsClients {
		con.mu.RLock()
		if con.modelNode != nil {
			syncz = append(syncz, SyncStatus{
				ProxyID:         con.modelNode.ID,
				IstioVersion:    con.modelNode.Metadata.IstioVersion,
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
	adsClientsMutex.RUnlock()
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
						p.Name, svc.Endpoint.Family, svc.Endpoint.Address, svc.Endpoint.Port, svc.Labels,
						svc.ServiceAccount)
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

// Config debugging.
func (s *DiscoveryServer) configz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	_, _ = fmt.Fprintf(w, "\n[\n")
	for _, typ := range s.Env.IstioConfigStore.ConfigDescriptor() {
		cfg, _ := s.Env.IstioConfigStore.List(typ.Type, "")
		for _, c := range cfg {
			b, err := json.MarshalIndent(c, "  ", "  ")
			if err != nil {
				return
			}
			_, _ = w.Write(b)
			_, _ = fmt.Fprint(w, ",\n")
		}
	}
	_, _ = fmt.Fprint(w, "\n{}]")
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

func configName(config *model.Config) string {
	if config != nil {
		return fmt.Sprintf("%s/%s", config.Name, config.Namespace)
	}
	return configNameNotApplicable
}

// Authenticationz dumps the authn tls-check info.
// This handler lists what authentication policy is used for a service and destination rules to
// that service that a proxy instance received.
// Proxy ID (<pod>.<namespace> need to be provided  to correctly  determine which destination rules
// are visible.
func (s *DiscoveryServer) Authenticationz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	proxyID := req.Form.Get("proxyID")
	adsClientsMutex.RLock()
	defer adsClientsMutex.RUnlock()

	connections, ok := adsSidecarIDConnectionsMap[proxyID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, "\n[\n]")
		return
	}
	var mostRecentProxy *model.Proxy
	mostRecent := ""
	for key := range connections {
		if mostRecent == "" || key > mostRecent {
			mostRecent = key
		}
	}
	mostRecentProxy = connections[mostRecent].modelNode
	svc, _ := s.Env.ServiceDiscovery.Services()
	info := []*AuthenticationDebug{}
	for _, ss := range svc {
		if ss.MeshExternal {
			// Skip external services
			continue
		}
		for _, p := range ss.Ports {
			authnPolicy := s.globalPushContext().AuthenticationPolicyForWorkload(ss, p)
			destConfig := s.globalPushContext().DestinationRule(mostRecentProxy, ss)
			info = append(info, AnalyzeMTLSSettings(ss.Hostname, p, authnPolicy, destConfig)...)
		}
	}

	if b, err := json.MarshalIndent(info, "  ", "  "); err == nil {
		_, _ = w.Write(b)
	}
}

// AnalyzeMTLSSettings returns mTLS compatibility status between client and server policies.
func AnalyzeMTLSSettings(hostname host.Name, port *model.Port, authnPolicy *authn.Policy,
	destConfig *model.Config) []*AuthenticationDebug {
	// TODO(diemvu): add policy config name to the cache push config for this.
	authnPolicyName := configNameNotApplicable
	if authnPolicy != nil {
		authnPolicyName = configNameUnknown
	}

	serverMTLSMode := authn_alpha1.GetMutualTLSMode(authnPolicy)

	baseDebugInfo := AuthenticationDebug{
		Port:                     port.Port,
		AuthenticationPolicyName: authnPolicyName,
		DestinationRuleName:      configName(destConfig),
		ServerProtocol:           serverMTLSMode.String(),
		ClientProtocol:           configNameNotApplicable,
	}

	var rule *networking.DestinationRule

	if destConfig != nil {
		rule = destConfig.Spec.(*networking.DestinationRule)
	}

	output := []*AuthenticationDebug{}

	clientTLSModes := collectTLSSettingsForPort(rule, port)
	var subsets []string
	for k := range clientTLSModes {
		subsets = append(subsets, k)
	}
	sort.Strings(subsets)

	for _, ss := range subsets {
		c := clientTLSModes[ss]
		info := baseDebugInfo
		if c != nil {
			info.ClientProtocol = c.GetMode().String()
		}
		if ss == "" {
			info.Host = string(hostname)
		} else {
			info.Host = fmt.Sprintf("%s|%s", hostname, ss)
		}
		info.TLSConflictStatus = EvaluateTLSState(c, serverMTLSMode)

		output = append(output, &info)
	}
	return output
}

// EvaluateTLSState returns the conflict state (string) for the input client+server settings.
// The output string could be:
// - "OK": both client and server TLS settings are set correctly.
// - "CONFLICT": both client and server TLS settings are set, but could be incompatible.
func EvaluateTLSState(clientMode *networking.TLSSettings, serverMode authn_alpha1.MutualTLSMode) string {
	const okState string = "OK"
	const conflictState string = "CONFLICT"

	if clientMode == nil {
		// TLS settings was not set explicitly, pilot will try a setting that work well with the
		// destination authN policy. We could use the separate state value (e.g AUTO) in the future.
		return okState
	}

	if (serverMode == authn_alpha1.MTLSDisable && clientMode.GetMode() == networking.TLSSettings_DISABLE) ||
		(serverMode == authn_alpha1.MTLSStrict && clientMode.GetMode() == networking.TLSSettings_ISTIO_MUTUAL) ||
		(serverMode == authn_alpha1.MTLSPermissive &&
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
		adsClientsMutex.RLock()
		_, _ = fmt.Fprintf(w, "Pushed to %d servers", len(adsClients))
		adsClientsMutex.RUnlock()
		return
	}
	writeAllADS(w)
}

// ConfigDump returns information in the form of the Envoy admin API config dump for the specified proxy
// The dump will only contain dynamic listeners/clusters/routes and can be used to compare what an Envoy instance
// should look like according to Pilot vs what it currently does look like.
func (s *DiscoveryServer) ConfigDump(w http.ResponseWriter, req *http.Request) {
	if proxyID := req.URL.Query().Get("proxyID"); proxyID != "" {
		adsClientsMutex.RLock()
		defer adsClientsMutex.RUnlock()
		connections, ok := adsSidecarIDConnectionsMap[proxyID]
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

// PushStatusHandler dumps the last PushContext
func (s *DiscoveryServer) PushStatusHandler(w http.ResponseWriter, req *http.Request) {
	if model.LastPushStatus == nil {
		return
	}
	out, err := model.LastPushStatus.JSON()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, "unable to marshal push information: %v", err)
		return
	}
	w.Header().Add("Content-Type", "application/json")

	_, _ = w.Write(out)
}

func writeAllADS(w io.Writer) {
	adsClientsMutex.RLock()
	defer adsClientsMutex.RUnlock()

	// Dirty json generation - because standard json is dirty (struct madness)
	// Unfortunately we must use the jsonbp to encode part of the json - I'm sure there are
	// better ways, but this is mainly for debugging.
	_, _ = fmt.Fprint(w, "[\n")
	comma := false
	for _, c := range adsClients {
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

func (s *DiscoveryServer) ready(w http.ResponseWriter, req *http.Request) {
	if s.ConfigController != nil {
		if !s.ConfigController.HasSynced() {
			w.WriteHeader(503)
			return
		}
	}
	if s.KubeController != nil {
		if !s.KubeController.HasSynced() {
			w.WriteHeader(503)
			return
		}
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

	edsClusterMutex.RLock()
	defer edsClusterMutex.RUnlock()
	comma := false
	if len(edsClusters) > 0 {
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
func cdsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	adsClientsMutex.RLock()

	_, _ = fmt.Fprint(w, "[\n")
	comma := false
	for _, c := range adsClients {
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

	adsClientsMutex.RUnlock()
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
