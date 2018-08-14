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
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"sync"

	"github.com/gogo/protobuf/jsonpb"

	authn "istio.io/api/authentication/v1alpha1"
	networking "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pilot/pkg/model"
	networking_core "istio.io/istio/pilot/pkg/networking/core/v1alpha3"
	authn_plugin "istio.io/istio/pilot/pkg/networking/plugin/authn"
	"istio.io/istio/pilot/pkg/serviceregistry"
	"istio.io/istio/pilot/pkg/serviceregistry/aggregate"
)

// memregistry is based on mock/discovery - it is used for testing and debugging v2.
// In future (post 1.0) it may be used for representing remote pilots.

// InitDebug initializes the debug handlers and adds a debug in-memory registry.
func (s *DiscoveryServer) InitDebug(mux *http.ServeMux, sctl *aggregate.Controller) {
	// For debugging and load testing v2 we add an memory registry.
	s.MemRegistry = NewMemServiceDiscovery(
		map[model.Hostname]*model.Service{ // mock.HelloService.Hostname: mock.HelloService,
		}, 2)

	sctl.AddRegistry(aggregate.Registry{
		ClusterID:        "v2-debug",
		Name:             serviceregistry.ServiceRegistry("memAdapter"),
		ServiceDiscovery: s.MemRegistry,
		ServiceAccounts:  s.MemRegistry,
		Controller:       s.MemRegistry.controller,
	})

	mux.HandleFunc("/ready", s.ready)

	mux.HandleFunc("/debug/edsz", s.edsz)
	mux.HandleFunc("/debug/adsz", s.adsz)
	mux.HandleFunc("/debug/cdsz", cdsz)
	mux.HandleFunc("/debug/syncz", Syncz)

	mux.HandleFunc("/debug/registryz", s.registryz)
	mux.HandleFunc("/debug/endpointz", s.endpointz)
	mux.HandleFunc("/debug/configz", s.configz)

	mux.HandleFunc("/debug/authenticationz", s.authenticationz)
	mux.HandleFunc("/debug/config_dump", s.ConfigDump)
	mux.HandleFunc("/debug/push_status", s.PushStatusHandler)
}

// NewMemServiceDiscovery builds an in-memory MemServiceDiscovery
func NewMemServiceDiscovery(services map[model.Hostname]*model.Service, versions int) *MemServiceDiscovery {
	return &MemServiceDiscovery{
		services:            services,
		versions:            versions,
		controller:          &memServiceController{},
		instancesByPortNum:  map[string][]*model.ServiceInstance{},
		instancesByPortName: map[string][]*model.ServiceInstance{},
		ip2instance:         map[string][]*model.ServiceInstance{},
	}
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
	w.WriteHeader(http.StatusOK)
}

// TODO: the mock was used for test setup, has no mutex. This will also be used for
// integration and load tests, will need to add mutex as we cleanup the code.

type memServiceController struct {
	svcHandlers  []func(*model.Service, model.Event)
	instHandlers []func(*model.ServiceInstance, model.Event)
}

func (c *memServiceController) AppendServiceHandler(f func(*model.Service, model.Event)) error {
	c.svcHandlers = append(c.svcHandlers, f)
	return nil
}

func (c *memServiceController) AppendInstanceHandler(f func(*model.ServiceInstance, model.Event)) error {
	c.instHandlers = append(c.instHandlers, f)
	return nil
}

func (c *memServiceController) Run(<-chan struct{}) {}

// MemServiceDiscovery is a mock discovery interface
type MemServiceDiscovery struct {
	services map[model.Hostname]*model.Service
	// Endpoints table. Key is the fqdn of the service, ':', port
	instancesByPortNum            map[string][]*model.ServiceInstance
	instancesByPortName           map[string][]*model.ServiceInstance
	ip2instance                   map[string][]*model.ServiceInstance
	versions                      int
	WantGetProxyServiceInstances  []*model.ServiceInstance
	ServicesError                 error
	GetServiceError               error
	InstancesError                error
	GetProxyServiceInstancesError error
	controller                    model.Controller

	// Single mutex for now - it's for debug only.
	mutex sync.Mutex
}

// ClearErrors clear errors used for mocking failures during model.MemServiceDiscovery interface methods
func (sd *MemServiceDiscovery) ClearErrors() {
	sd.ServicesError = nil
	sd.GetServiceError = nil
	sd.InstancesError = nil
	sd.GetProxyServiceInstancesError = nil
}

// AddService adds an in-memory service.
func (sd *MemServiceDiscovery) AddService(name model.Hostname, svc *model.Service) {
	sd.mutex.Lock()
	sd.services[name] = svc
	sd.mutex.Unlock()
	// TODO: notify listeners
}

// AddInstance adds an in-memory instance.
func (sd *MemServiceDiscovery) AddInstance(service model.Hostname, instance *model.ServiceInstance) {
	// WIP: add enough code to allow tests and load tests to work
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	svc := sd.services[service]
	if svc == nil {
		return
	}
	instance.Service = svc
	sd.ip2instance[instance.Endpoint.Address] = []*model.ServiceInstance{instance}

	key := fmt.Sprintf("%s:%d", service, instance.Endpoint.ServicePort.Port)
	instanceList := sd.instancesByPortNum[key]
	sd.instancesByPortNum[key] = append(instanceList, instance)

	key = fmt.Sprintf("%s:%s", service, instance.Endpoint.ServicePort.Name)
	instanceList = sd.instancesByPortName[key]
	sd.instancesByPortName[key] = append(instanceList, instance)
}

// AddEndpoint adds an endpoint to a service.
func (sd *MemServiceDiscovery) AddEndpoint(service model.Hostname, servicePortName string, servicePort int, address string, port int) *model.ServiceInstance {
	instance := &model.ServiceInstance{
		Endpoint: model.NetworkEndpoint{
			Address: address,
			Port:    port,
			ServicePort: &model.Port{
				Name:     servicePortName,
				Port:     servicePort,
				Protocol: model.ProtocolHTTP,
			},
		},
	}
	sd.AddInstance(service, instance)
	return instance
}

// Services implements discovery interface
// Each call to Services() should return a list of new *model.Service
func (sd *MemServiceDiscovery) Services() ([]*model.Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.ServicesError != nil {
		return nil, sd.ServicesError
	}
	out := make([]*model.Service, 0, len(sd.services))
	for _, service := range sd.services {
		// Make a new service out of the existing one
		newSvc := *service
		out = append(out, &newSvc)
	}
	return out, sd.ServicesError
}

// GetService implements discovery interface
// Each call to GetService() should return a new *model.Service
func (sd *MemServiceDiscovery) GetService(hostname model.Hostname) (*model.Service, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetServiceError != nil {
		return nil, sd.GetServiceError
	}
	val := sd.services[hostname]
	if val == nil {
		return nil, errors.New("missing service")
	}
	// Make a new service out of the existing one
	newSvc := *val
	return &newSvc, sd.GetServiceError
}

// Instances filters the service instances by labels. This assumes single port, as is
// used by EDS/ADS.
func (sd *MemServiceDiscovery) Instances(hostname model.Hostname, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	if len(ports) != 1 {
		adsLog.Warna("Unexpected ports ", ports)
		return nil, nil
	}
	key := string(hostname) + ":" + ports[0]
	instances, ok := sd.instancesByPortName[key]
	if !ok {
		return nil, nil
	}
	return instances, nil
}

// InstancesByPort filters the service instances by labels. This assumes single port, as is
// used by EDS/ADS.
func (sd *MemServiceDiscovery) InstancesByPort(hostname model.Hostname, port int,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.InstancesError != nil {
		return nil, sd.InstancesError
	}
	key := fmt.Sprintf("%s:%d", string(hostname), port)
	instances, ok := sd.instancesByPortNum[key]
	if !ok {
		return nil, nil
	}
	return instances, nil
}

// GetProxyServiceInstances returns service instances associated with a node, resulting in
// 'in' services.
func (sd *MemServiceDiscovery) GetProxyServiceInstances(node *model.Proxy) ([]*model.ServiceInstance, error) {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if sd.GetProxyServiceInstancesError != nil {
		return nil, sd.GetProxyServiceInstancesError
	}
	if sd.WantGetProxyServiceInstances != nil {
		return sd.WantGetProxyServiceInstances, nil
	}
	out := make([]*model.ServiceInstance, 0)
	si, found := sd.ip2instance[node.IPAddress]
	if found {
		out = append(out, si...)
	}
	return out, sd.GetProxyServiceInstancesError
}

// ManagementPorts implements discovery interface
func (sd *MemServiceDiscovery) ManagementPorts(addr string) model.PortList {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	return model.PortList{{
		Name:     "http",
		Port:     3333,
		Protocol: model.ProtocolHTTP,
	}, {
		Name:     "custom",
		Port:     9999,
		Protocol: model.ProtocolTCP,
	}}
}

// WorkloadHealthCheckInfo implements discovery interface
func (sd *MemServiceDiscovery) WorkloadHealthCheckInfo(addr string) model.ProbeList {
	return nil
}

// GetIstioServiceAccounts gets the Istio service accounts for a service hostname.
func (sd *MemServiceDiscovery) GetIstioServiceAccounts(hostname model.Hostname, ports []string) []string {
	sd.mutex.Lock()
	defer sd.mutex.Unlock()
	if hostname == "world.default.svc.cluster.local" {
		return []string{
			"spiffe://cluster.local/ns/default/sa/serviceaccount1",
			"spiffe://cluster.local/ns/default/sa/serviceaccount2",
		}
	}
	return make([]string, 0)
}

// registryz providees debug support for registry - adding and listing model items.
// Can be combined with the push debug interface to reproduce changes.
func (s *DiscoveryServer) registryz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	svcName := req.Form.Get("svc")
	if svcName != "" {
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return
		}
		svc := &model.Service{}
		err = json.Unmarshal(data, svc)
		if err != nil {
			return
		}
		s.MemRegistry.AddService(model.Hostname(svcName), svc)
	}

	all, err := s.env.ServiceDiscovery.Services()
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

// Endpoint debugging
func (s *DiscoveryServer) endpointz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	svcName := req.Form.Get("svc")
	if svcName != "" {
		data, err := ioutil.ReadAll(req.Body)
		if err != nil {
			return
		}
		svc := &model.ServiceInstance{}
		err = json.Unmarshal(data, svc)
		if err != nil {
			return
		}
		s.MemRegistry.AddInstance(model.Hostname(svcName), svc)
	}
	brief := req.Form.Get("brief")
	if brief != "" {
		svc, _ := s.env.ServiceDiscovery.Services()
		for _, ss := range svc {
			for _, p := range ss.Ports {
				all, err := s.env.ServiceDiscovery.InstancesByPort(ss.Hostname, p.Port, nil)
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

	svc, _ := s.env.ServiceDiscovery.Services()
	fmt.Fprint(w, "[\n")
	for _, ss := range svc {
		for _, p := range ss.Ports {
			all, err := s.env.ServiceDiscovery.InstancesByPort(ss.Hostname, p.Port, nil)
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
	for _, typ := range s.env.IstioConfigStore.ConfigDescriptor() {
		cfg, _ := s.env.IstioConfigStore.List(typ.Type, "")
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

// Returns whether the given destination rule use (Istio) mutual TLS setting for given port.
// TODO: check subsets possibly conflicts between subsets.
func isMTlsOn(rule *networking.DestinationRule, port *model.Port) bool {
	if rule.TrafficPolicy == nil {
		return false
	}
	_, _, _, tls := networking_core.SelectTrafficPolicyComponents(rule.TrafficPolicy, port)

	return tls != nil && tls.Mode == networking.TLSSettings_ISTIO_MUTUAL
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

func mTLSModeToString(useTLS bool) string {
	if useTLS {
		return "mTLS"
	}
	return "HTTP"
}

// Authentication debugging
// This handler lists what authentication policy and destination rules is used for a service, and
// whether or not they have TLS setting conflicts (i.e authentication policy use mutual TLS, but
// destination rule doesn't use ISTIO_MUTUAL TLS mode). If service is not provided, (via request
// paramerter `svc`), it lists result for all services.
func (s *DiscoveryServer) authenticationz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")
	// This should be svc. However, use proxyID param for now so it can be used with
	// `pilot-discovery debug` command
	interestedSvc := req.Form.Get("proxyID")

	fmt.Fprintf(w, "\n[\n")
	svc, _ := s.env.ServiceDiscovery.Services()
	for _, ss := range svc {
		if interestedSvc != "" && interestedSvc != string(ss.Hostname) {
			continue
		}
		for _, p := range ss.Ports {
			info := AuthenticationDebug{
				Host: string(ss.Hostname),
				Port: p.Port,
			}
			authnConfig := s.env.IstioConfigStore.AuthenticationPolicyByDestination(ss, p)
			info.AuthenticationPolicyName = configName(authnConfig)
			if authnConfig != nil {
				policy := authnConfig.Spec.(*authn.Policy)
				mtls := authn_plugin.GetMutualTLS(policy, model.Sidecar)
				info.ServerProtocol = mTLSModeToString(mtls != nil)
			} else {
				info.ServerProtocol = mTLSModeToString(false)
			}

			destConfig := s.env.PushContext.DestinationRule(ss.Hostname)
			info.DestinationRuleName = configName(destConfig)
			if destConfig != nil {
				rule := destConfig.Spec.(*networking.DestinationRule)
				info.ClientProtocol = mTLSModeToString(isMTlsOn(rule, p))
			} else {
				info.ClientProtocol = mTLSModeToString(false)
			}

			if info.ClientProtocol != info.ServerProtocol {
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
		if !ok {
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
	w.WriteHeader(http.StatusOK)
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
		}
	}
	w.WriteHeader(200)
}

// edsz implements a status and debug interface for EDS.
// It is mapped to /debug/edsz on the monitor port (9093).
func (s *DiscoveryServer) edsz(w http.ResponseWriter, req *http.Request) {
	_ = req.ParseForm()
	w.Header().Add("Content-Type", "application/json")

	if req.Form.Get("push") != "" {
		AdsPushAll(s)
	}

	edsClusterMutex.Lock()
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
	edsClusterMutex.Unlock()
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
