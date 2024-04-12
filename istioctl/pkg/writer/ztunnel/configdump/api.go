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
	WorkloadIPs       []string  `json:"workloadIps"`
	Waypoint          *Waypoint `json:"waypoint"`
	Protocol          string    `json:"protocol"`
	Name              string    `json:"name"`
	Namespace         string    `json:"namespace"`
	ServiceAccount    string    `json:"serviceAccount"`
	WorkloadName      string    `json:"workloadName"`
	WorkloadType      string    `json:"workloadType"`
	CanonicalName     string    `json:"canonicalName"`
	CanonicalRevision string    `json:"canonicalRevision"`
	ClusterID         string    `json:"clusterId"`
	TrustDomain       string    `json:"trustDomain,omitempty"`
	Locality          Locality  `json:"locality,omitempty"`
	Node              string    `json:"node"`
	Network           string    `json:"network,omitempty"`
	Status            string    `json:"status"`
}

type Waypoint struct {
	Destination string `json:"destination"`
}

type LoadBalancer struct {
	Mode               string   `json:"mode"`
	RoutingPreferences []string `json:"routingPreferences"`
}

type ZtunnelService struct {
	Name         string         `json:"name"`
	Namespace    string         `json:"namespace"`
	Hostname     string         `json:"hostname"`
	Addresses    []string       `json:"vips"`
	Ports        map[string]int `json:"ports"`
	LoadBalancer *LoadBalancer  `json:"loadBalancer"`
	Waypoint     *Waypoint      `json:"waypoint"`
}

type ZtunnelDump struct {
	Workloads    map[string]*ZtunnelWorkload `json:"by_addr"`
	Services     map[string]*ZtunnelService  `json:"by_vip"`
	Certificates []*CertsDump                `json:"certificates"`
}

type CertsDump struct {
	Identity  string  `json:"identity"`
	State     string  `json:"state"`
	CertChain []*Cert `json:"cert_chain"`
}

type Cert struct {
	Pem            string `json:"pem"`
	SerialNumber   string `json:"serial_number"`
	ValidFrom      string `json:"valid_from"`
	ExpirationTime string `json:"expiration_time"`
}
