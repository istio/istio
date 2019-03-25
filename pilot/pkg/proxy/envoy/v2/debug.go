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

	"github.com/gogo/protobuf/jsonpb"

	authn "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	networking_core "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	authn_plugin "istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = NewMemServiceDiscovery(
		map[model.Hostname]*model.Service{ // mock.HelloService.Hostname: mock.HelloService,
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
	mux.HandleFunc("/debug/workloadz", s.workloadz)
	mux.HandleFunc("/debug/configz", s.configz)

	mux.HandleFunc("/debug/authenticationz", s.authenticationz)
	mux.HandleFunc("/debug/config_dump", s.ConfigDump)
	mux.HandleFunc("/debug/push_status", s.PushStatusHandler)
}

// SyncStatus is the synchronization status between Pilot and a given Envoy
type SyncStatus struct {
	ProxyID         string `json:"proxy,omitempty"`
	ProxyVersion    string `json:"proxy_version,omitempty"`
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
func Syncz(w http.ResponseWriter, req *http.Request) {
	syncz := []SyncStatus{}
	adsClientsMutex.RLock()
	for _, con := range adsClients {
		con.mu.RLock()
		if con.modelNode != nil {
			proxyVersion, _ := con.modelNode.GetProxyVersion()
			syncz = append(syncz, SyncStatus{
				ProxyID:         con.modelNode.ID,
				ProxyVersion:    proxyVersion,
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
		fmt.Fprintf(w, "unable to marshal syncz information: %v", err)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	w.Write(out)
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
	fmt.Fprintln(w, "[")
	for _, svc := range all {
		b, err := json.MarshalIndent(svc, "", "  ")
		if err != nil {
			return
		}
		_, _ = w.Write(b)
		fmt.Fprintln(w, ",")
	}
	fmt.Fprintln(w, "{}]")
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
	w.Write(out)
}

// Tracks info about workloads. Currently only K8S serviceregistry populates this, based
// on pod labels and annotations. This is used to detect label changes and push.
func (s *DiscoveryServer) workloadz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	s.mutex.RLock()
	out, _ := json.MarshalIndent(s.WorkloadsByID, " ", " ")
	s.mutex.RUnlock()
	w.Write(out)
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
				all, err := s.Env.ServiceDiscovery.InstancesByPort(ss.Hostname, p.Port, nil)
				if err != nil {
					return
				}
				for _, svc := range all {
					fmt.Fprintf(w, "%s:%s %v %s:%d %v %s\n", ss.Hostname,
						p.Name, svc.Endpoint.Family, svc.Endpoint.Address, svc.Endpoint.Port, svc.Labels,
						svc.ServiceAccount)
				}
			}
		}
		return
	}

	svc, _ := s.Env.ServiceDiscovery.Services()
	fmt.Fprint(w, "[\n")
	for _, ss := range svc {
		for _, p := range ss.Ports {
			all, err := s.Env.ServiceDiscovery.InstancesByPort(ss.Hostname, p.Port, nil)
			if err != nil {
				return
			}
			fmt.Fprintf(w, "\n{\"svc\": \"%s:%s\", \"ep\": [\n", ss.Hostname, p.Name)
			for _, svc := range all {
				b, err := json.MarshalIndent(svc, "  ", "  ")
				if err != nil {
					return
				}
				_, _ = w.Write(b)
				fmt.Fprint(w, ",\n")
			}
			fmt.Fprint(w, "\n{}]},")
		}
	}
	fmt.Fprint(w, "\n{}]\n")
}

// Config debugging.
func (s *DiscoveryServer) configz(w http.ResponseWriter, req *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	fmt.Fprintf(w, "\n[\n")
	for _, typ := range s.Env.IstioConfigStore.ConfigDescriptor() {
		cfg, _ := s.Env.IstioConfigStore.List(typ.Type, "")
		for _, c := range cfg {
			b, err := json.MarshalIndent(c, "  ", "  ")
			if err != nil {
				return
			}
			_, _ = w.Write(b)
			fmt.Fprint(w, ",\n")
		}
	}
	fmt.Fprint(w, "\n{}]")
}

type authProtocol int

const (
	authnHTTP       authProtocol = 1
	authnMTls       authProtocol = 2
	authnPermissive authProtocol = authnHTTP | authnMTls
)

// Returns whether the given destination rule use (Istio) mutual TLS setting for given port.
// TODO: check subsets possibly conflicts between subsets.
func clientAuthProtocol(rule *networking.DestinationRule, port *model.Port) authProtocol {
	if rule.TrafficPolicy == nil {
		return authnHTTP
	}
	_, _, _, tls := networking_core.SelectTrafficPolicyComponents(rule.TrafficPolicy, port)

	if tls != nil && tls.Mode == networking.TLSSettings_ISTIO_MUTUAL {
		return authnMTls
	}
	return authnHTTP
}

func getServerAuthProtocol(mtls *authn.MutualTls) authProtocol {
	if mtls == nil {
		return authnHTTP
	}
	if mtls.Mode == authn.MutualTls_PERMISSIVE {
		return authnPermissive
	}
	return authnMTls
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

func configName(config *model.Config) string {
	if config != nil {
		return fmt.Sprintf("%s/%s", config.Name, config.Namespace)
	}
	return "-"
}

func authProtocolToString(protocol authProtocol) string {
	switch protocol {
	case authnHTTP:
		return "HTTP"
	case authnMTls:
		return "mTLS"
	case authnPermissive:
		return "HTTP/mTLS"
	default:
		return "UNKNOWN"
	}
}

// Authentication debugging
// This handler lists what authentication policy is used for a service and destination rules to
// that service that a proxy instance received.
// Proxy ID (<pod>.<namespace> need to be provided  to correctly  determine which destination rules
// are visible.
func (s *DiscoveryServer) authenticationz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	proxyID := req.Form.Get("proxyID")
	adsClientsMutex.RLock()
	defer adsClientsMutex.RUnlock()

	connections, ok := adsSidecarIDConnectionsMap[proxyID]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "\n[\n]")
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
	fmt.Fprintf(w, "\n[\n")
	svc, _ := s.Env.ServiceDiscovery.Services()
	for _, ss := range svc {
		for _, p := range ss.Ports {
			info := AuthenticationDebug{
				Host: string(ss.Hostname),
				Port: p.Port,
			}
			authnConfig := s.Env.IstioConfigStore.AuthenticationPolicyByDestination(ss, p)
			info.AuthenticationPolicyName = configName(authnConfig)
			var serverProtocol, clientProtocol authProtocol
			if authnConfig != nil {
				policy := authnConfig.Spec.(*authn.Policy)
				mtls := authn_plugin.GetMutualTLS(policy)
				serverProtocol = getServerAuthProtocol(mtls)
			} else {
				serverProtocol = getServerAuthProtocol(nil)
			}
			info.ServerProtocol = authProtocolToString(serverProtocol)

			destConfig := s.globalPushContext().DestinationRule(mostRecentProxy, ss)
			info.DestinationRuleName = configName(destConfig)
			if destConfig != nil {
				rule := destConfig.Spec.(*networking.DestinationRule)
				clientProtocol = clientAuthProtocol(rule, p)
			} else {
				clientProtocol = authnHTTP
			}
			info.ClientProtocol = authProtocolToString(clientProtocol)

			if (clientProtocol & serverProtocol) == 0 {
				info.TLSConflictStatus = "CONFLICT"
			} else {
				info.TLSConflictStatus = "OK"
			}
			if b, err := json.MarshalIndent(info, "  ", "  "); err == nil {
				_, _ = w.Write(b)
			}
			fmt.Fprintf(w, ",\n")
		}
	}
	fmt.Fprint(w, "\n{}]")
}

// adsz implements a status and debug interface for ADS.
// It is mapped to /debug/adsz
func (s *DiscoveryServer) adsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	if req.Form.Get("push") != "" {
		AdsPushAll(s)
		adsClientsMutex.RLock()
		fmt.Fprintf(w, "Pushed to %d servers", len(adsClients))
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
			w.Write([]byte("Proxy not connected to this Pilot instance"))
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
			w.Write([]byte(err.Error()))
			return
		}
		if err := jsonm.Marshal(w, dump); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(err.Error()))
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	w.WriteHeader(http.StatusBadRequest)
	w.Write([]byte("You must provide a proxyID in the query string"))
}

// PushStatusHandler dumps the last PushContext
func (s *DiscoveryServer) PushStatusHandler(w http.ResponseWriter, req *http.Request) {
	if model.LastPushStatus == nil {
		return
	}
	out, err := model.LastPushStatus.JSON()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, "unable to marshal push information: %v", err)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	w.Write(out)
}

func writeAllADS(w io.Writer) {
	adsClientsMutex.RLock()
	defer adsClientsMutex.RUnlock()

	// Dirty json generation - because standard json is dirty (struct madness)
	// Unfortunately we must use the jsonbp to encode part of the json - I'm sure there are
	// better ways, but this is mainly for debugging.
	fmt.Fprint(w, "[\n")
	comma := false
	for _, c := range adsClients {
		if comma {
			fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		fmt.Fprintf(w, "\n\n  {\"node\": \"%s\",\n \"addr\": \"%s\",\n \"connect\": \"%v\",\n \"listeners\":[\n", c.ConID, c.PeerAddr, c.Connect)
		printListeners(w, c)
		fmt.Fprint(w, "],\n")
		fmt.Fprintf(w, "\"RDSRoutes\":[\n")
		printRoutes(w, c)
		fmt.Fprint(w, "],\n")
		fmt.Fprintf(w, "\"clusters\":[\n")
		printClusters(w, c)
		fmt.Fprint(w, "]}\n")
	}
	fmt.Fprint(w, "]\n")
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
	comma := false
	if len(edsClusters) > 0 {
		fmt.Fprintln(w, "[")
		for _, eds := range edsClusters {
			if comma {
				fmt.Fprint(w, ",\n")
			} else {
				comma = true
			}
			jsonm := &jsonpb.Marshaler{Indent: "  "}
			dbgString, _ := jsonm.MarshalToString(eds.LoadAssignment)
			if _, err := w.Write([]byte(dbgString)); err != nil {
				return
			}
		}
		fmt.Fprintln(w, "]")
	} else {
		w.WriteHeader(404)
	}
	edsClusterMutex.RUnlock()
}

// cdsz implements a status and debug interface for CDS.
// It is mapped to /debug/cdsz
func cdsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	adsClientsMutex.RLock()

	fmt.Fprint(w, "[\n")
	comma := false
	for _, c := range adsClients {
		if comma {
			fmt.Fprint(w, ",\n")
		} else {
			comma = true
		}
		fmt.Fprintf(w, "\n\n  {\"node\": \"%s\", \"addr\": \"%s\", \"connect\": \"%v\",\"Clusters\":[\n", c.ConID, c.PeerAddr, c.Connect)
		printClusters(w, c)
		fmt.Fprint(w, "]}\n")
	}
	fmt.Fprint(w, "]\n")

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
			fmt.Fprint(w, ",\n")
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
			fmt.Fprint(w, ",\n")
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
			fmt.Fprint(w, ",\n")
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
