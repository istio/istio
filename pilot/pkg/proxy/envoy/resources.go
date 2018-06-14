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

package envoy

// RootRuntime definition.
// See https://envoyproxy.github.io/envoy/configuration/overview/overview.html
type RootRuntime struct {
	SymlinkRoot          string `json:"symlink_root"`
	Subdirectory         string `json:"subdirectory"`
	OverrideSubdirectory string `json:"override_subdirectory,omitempty"`
}

// Runtime definition
type Runtime struct {
	Key     string `json:"key"`
	Default int    `json:"default"`
}

// AccessLog definition.
type AccessLog struct {
	Path   string `json:"path"`
	Format string `json:"format,omitempty"`
	Filter string `json:"filter,omitempty"`
}

// Tracing definition
type Tracing struct {
	HTTPTracer HTTPTracer `json:"http"`
}

// HTTPTracer definition
type HTTPTracer struct {
	HTTPTraceDriver HTTPTraceDriver `json:"driver"`
}

// HTTPTraceDriver definition
type HTTPTraceDriver struct {
	HTTPTraceDriverType   string                `json:"type"`
	HTTPTraceDriverConfig HTTPTraceDriverConfig `json:"config"`
}

// HTTPTraceDriverConfig definition
type HTTPTraceDriverConfig struct {
	CollectorCluster  string `json:"collector_cluster"`
	CollectorEndpoint string `json:"collector_endpoint"`
}

// Listener definition
type Listener struct {
	Address        string `json:"address"`
	Name           string `json:"name,omitempty"`
	BindToPort     bool   `json:"bind_to_port"`
	UseOriginalDst bool   `json:"use_original_dst,omitempty"`
}

// Cluster definition
type Cluster struct {
	Name                     string      `json:"name"`
	ServiceName              string      `json:"service_name,omitempty"`
	ConnectTimeoutMs         int64       `json:"connect_timeout_ms"`
	Type                     string      `json:"type"`
	LbType                   string      `json:"lb_type"`
	MaxRequestsPerConnection int         `json:"max_requests_per_connection,omitempty"`
	Hosts                    []Host      `json:"hosts,omitempty"`
	SSLContext               interface{} `json:"ssl_context,omitempty"`
}

// SSLContextWithSAN definition, VerifySubjectAltName cannot be nil.
type SSLContextWithSAN struct {
	CertChainFile        string   `json:"cert_chain_file"`
	PrivateKeyFile       string   `json:"private_key_file"`
	CaCertFile           string   `json:"ca_cert_file,omitempty"`
	VerifySubjectAltName []string `json:"verify_subject_alt_name"`
}

// Admin definition
type Admin struct {
	AccessLogPath string `json:"access_log_path"`
	Address       string `json:"address"`
}

// Host definition
type Host struct {
	URL string `json:"url"`
}

// Listeners is a collection of listeners
type Listeners []*Listener

// Clusters is a collection of clusters
type Clusters []*Cluster

// DiscoveryCluster is a service discovery service definition
type DiscoveryCluster struct {
	Cluster        *Cluster `json:"cluster"`
	RefreshDelayMs int64    `json:"refresh_delay_ms"`
}

// LDSCluster is a reference to LDS cluster by name
type LDSCluster struct {
	Cluster        string `json:"cluster"`
	RefreshDelayMs int64  `json:"refresh_delay_ms"`
}

// RDS definition
type RDS struct {
	Cluster         string `json:"cluster"`
	RouteConfigName string `json:"route_config_name"`
	RefreshDelayMs  int64  `json:"refresh_delay_ms"`
}

// ClusterManager definition
type ClusterManager struct {
	Clusters         Clusters          `json:"clusters"`
	SDS              *DiscoveryCluster `json:"sds,omitempty"`
	CDS              *DiscoveryCluster `json:"cds,omitempty"`
	LocalClusterName string            `json:"local_cluster_name,omitempty"`
}
