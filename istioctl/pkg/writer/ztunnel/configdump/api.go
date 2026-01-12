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

package configdump

type Locality struct {
	Region  string `json:"region,omitempty"`
	Zone    string `json:"zone,omitempty"`
	Subzone string `json:"subzone,omitempty"`
}

type ZtunnelWorkload struct {
	UID                   string            `json:"uid"`
	WorkloadIPs           []string          `json:"workloadIps"`
	Waypoint              *GatewayAddress   `json:"waypoint,omitempty"`
	NetworkGateway        *GatewayAddress   `json:"networkGateway,omitempty"`
	Protocol              string            `json:"protocol"`
	Name                  string            `json:"name"`
	Namespace             string            `json:"namespace"`
	ServiceAccount        string            `json:"serviceAccount"`
	WorkloadName          string            `json:"workloadName"`
	WorkloadType          string            `json:"workloadType"`
	CanonicalName         string            `json:"canonicalName"`
	CanonicalRevision     string            `json:"canonicalRevision"`
	ClusterID             string            `json:"clusterId"`
	TrustDomain           string            `json:"trustDomain,omitempty"`
	Locality              Locality          `json:"locality,omitempty"`
	Node                  string            `json:"node"`
	Network               string            `json:"network,omitempty"`
	Status                string            `json:"status"`
	Hostname              string            `json:"hostname"`
	Capacity              uint32            `json:"capacity"`
	ApplicationTunnel     ApplicationTunnel `json:"applicationTunnel,omitempty"`
	AuthorizationPolicies []string          `json:"authorizationPolicies,omitempty"`
}

type ApplicationTunnel struct {
	Protocol string  `json:"protocol"`
	Port     *uint16 `json:"port,omitempty"`
}

type GatewayAddress struct {
	Destination string `json:"destination"`
}

type LoadBalancer struct {
	Mode               string   `json:"mode"`
	RoutingPreferences []string `json:"routingPreferences"`
	HealthPolicy       string   `json:"healthPolicy"`
}

type ZtunnelEndpoint struct {
	WorkloadUID string            `json:"workloadUid"`
	Service     string            `json:"service"`
	Address     string            `json:"address,omitempty"`
	Port        map[uint16]uint16 `json:"port"`
}

type ZtunnelService struct {
	Name            string                      `json:"name"`
	Namespace       string                      `json:"namespace"`
	Hostname        string                      `json:"hostname"`
	Addresses       []string                    `json:"vips"`
	Ports           map[string]int              `json:"ports"`
	LoadBalancer    *LoadBalancer               `json:"loadBalancer,omitempty"`
	Waypoint        *GatewayAddress             `json:"waypoint,omitempty"`
	Endpoints       map[string]*ZtunnelEndpoint `json:"endpoints"`
	SubjectAltNames []string                    `json:"subjectAltNames,omitempty"`
	IPFamilies      string                      `json:"ipFamilies"`
}

type PolicyMatch struct {
	Namespaces          []StringMatch         `json:"namespaces,omitempty"`
	NotNamespaces       []StringMatch         `json:"notNamespaces,omitempty"`
	Principals          []StringMatch         `json:"principals,omitempty"`
	NotPrincipals       []StringMatch         `json:"notPrincipals,omitempty"`
	SourceIps           []string              `json:"sourceIps,omitempty"`
	NotSourceIps        []string              `json:"notSourceIps,omitempty"`
	DestinationIps      []string              `json:"destinationIps,omitempty"`
	NotDestinationIps   []string              `json:"notDestinationIps,omitempty"`
	DestinationPorts    []uint16              `json:"destinationPorts,omitempty"`
	NotDestinationPorts []uint16              `json:"notDestinationPorts,omitempty"`
	ServiceAccounts     []ServiceAccountMatch `json:"serviceAccounts,omitempty"`
	NotServiceAccounts  []ServiceAccountMatch `json:"notServiceAccounts,omitempty"`
}

type StringMatch struct {
	Exact    string `json:"Exact,omitempty"`
	Suffix   string `json:"Suffix,omitempty"`
	Prefix   string `json:"Prefix,omitempty"`
	Presence any    `json:"Presence,omitempty"`
}

type ServiceAccountMatch struct {
	Namespace      string `json:"namespace"`
	ServiceAccount string `json:"serviceAccount"`
}

type ZtunnelPolicy struct {
	Name      string             `json:"name"`
	Namespace string             `json:"namespace"`
	Scope     string             `json:"scope"`
	Action    string             `json:"action"`
	Rules     [][][]*PolicyMatch `json:"rules"`
}

type ZtunnelDump struct {
	Workloads     []*ZtunnelWorkload       `json:"workloads"`
	Services      []*ZtunnelService        `json:"services"`
	Policies      []*ZtunnelPolicy         `json:"policies"`
	Certificates  []*CertsDump             `json:"certificates"`
	WorkloadState map[string]WorkloadState `json:"workloadState"`
}

type CertsDump struct {
	Identity  string  `json:"identity"`
	State     string  `json:"state"`
	CertChain []*Cert `json:"certChain"`
	RootCert  []*Cert `json:"rootCerts"`
}

type Cert struct {
	Pem            string `json:"pem"`
	SerialNumber   string `json:"serialNumber"`
	ValidFrom      string `json:"validFrom"`
	ExpirationTime string `json:"expirationTime"`
}

type WorkloadState struct {
	State       string              `json:"state,omitempty"`
	Connections WorkloadConnections `json:"connections,omitempty"`
	Info        WorkloadInfo        `json:"info"`
}

type WorkloadConnections struct {
	Inbound  []InboundConnection  `json:"inbound"`
	Outbound []OutboundConnection `json:"outbound"`
}

type WorkloadInfo struct {
	Name           string `json:"name"`
	Namespace      string `json:"namespace"`
	TrustDomain    string `json:"trustDomain"`
	ServiceAccount string `json:"serviceAccount"`
}

type InboundConnection struct {
	Src         string `json:"src"`
	OriginalDst string `json:"originalDst"`
	ActualDst   string `json:"actualDst"`
	Protocol    string `json:"protocol"`
}

type OutboundConnection struct {
	Src         string `json:"src"`
	OriginalDst string `json:"originalDst"`
	ActualDst   string `json:"actualDst"`
	Protocol    string `json:"protocol"`
}

type WorkloadConnection struct {
	Src         string `json:"src"`
	Dst         string `json:"dst"`
	SrcIdentity string `json:"src_identity"`
	DstNetwork  string `json:"dst_network"`
}
