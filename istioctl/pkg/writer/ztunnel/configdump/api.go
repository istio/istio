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
	NativeHbone       bool      `json:"nativeHbone"`
	Network           string    `json:"network,omitempty"`
	Status            string    `json:"status"`
}

type Waypoint struct {
	Destination string `json:"destination"`
}

type ZtunnelService struct {
	Name      string         `json:"name"`
	Namespace string         `json:"namespace"`
	Hostname  string         `json:"hostname"`
	Addresses []string       `json:"addresses"`
	Ports     map[string]int `json:"ports"`
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
