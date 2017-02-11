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

import (
	"sort"

	"istio.io/manager/model"
)

// MeshConfig defines proxy mesh variables
type MeshConfig struct {
	// DiscoveryAddress is the DNS address for Envoy discovery service
	DiscoveryAddress string
	// MixerAddress is the authority address for Istio Mixer service (HOST:PORT)
	MixerAddress string
	// ProxyPort is the Envoy proxy port
	ProxyPort int
	// AdminPort is the administrative interface port
	AdminPort int
	// Envoy binary path
	BinaryPath string
	// Envoy config root path
	ConfigPath string
	// Envoy runtime config path
	RuntimePath string
	// Envoy access log path
	AccessLogPath string
}

// TODO: these values used in the Envoy configuration will be configurable
const (
	DefaultTimeoutMs = 1000
	DefaultLbType    = LbTypeRoundRobin
	DefaultAccessLog = "/dev/stdout"
	LbTypeRoundRobin = "round_robin"

	// HTTPConnectionManager is the name of HTTP filter
	HTTPConnectionManager = "http_connection_manager"

	// URI HTTP header
	HeaderURI = "uri"
)

// Config defines the schema for Envoy JSON configuration format
type Config struct {
	RootRuntime    *RootRuntime   `json:"runtime,omitempty"`
	Listeners      []*Listener    `json:"listeners"`
	Admin          Admin          `json:"admin"`
	ClusterManager ClusterManager `json:"cluster_manager"`
}

// RootRuntime definition.
// See: https://lyft.github.io/envoy/docs/configuration/overview/overview.html
type RootRuntime struct {
	SymlinkRoot          string `json:"symlink_root"`
	Subdirectory         string `json:"subdirectory"`
	OverrideSubdirectory string `json:"override_subdirectory,omitempty"`
}

// AbortFilter definition
type AbortFilter struct {
	Percent    int `json:"abort_percent,omitempty"`
	HTTPStatus int `json:"http_status,omitempty"`
}

// DelayFilter definition
type DelayFilter struct {
	Type     string `json:"type,omitempty"`
	Percent  int    `json:"fixed_delay_percent,omitempty"`
	Duration int    `json:"fixed_duration_ms,omitempty"`
}

// Header definition
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
	Regex bool   `json:"regex,omitempty"`
}

// FilterMixerConfig definition
type FilterMixerConfig struct {
	MixerServer string `json:"mixer_server"`
}

// FilterFaultConfig definition
type FilterFaultConfig struct {
	Abort           *AbortFilter `json:"abort,omitempty"`
	Delay           *DelayFilter `json:"delay,omitempty"`
	Headers         Headers      `json:"headers,omitempty"`
	UpstreamCluster string       `json:"upstream_cluster,omitempty"`
}

// FilterRouterConfig definition
type FilterRouterConfig struct {
	// DynamicStats defaults to true
	DynamicStats bool `json:"dynamic_stats,omitempty"`
}

// Filter definition
type Filter struct {
	Type   string      `json:"type"`
	Name   string      `json:"name"`
	Config interface{} `json:"config"`
}

// Runtime definition
type Runtime struct {
	Key     string `json:"key"`
	Default int    `json:"default"`
}

// Route definition
type Route struct {
	Runtime       *Runtime `json:"runtime,omitempty"`
	Path          string   `json:"path,omitempty"`
	Prefix        string   `json:"prefix,omitempty"`
	PrefixRewrite string   `json:"prefix_rewrite,omitempty"`

	Cluster          string           `json:"cluster"`
	WeightedClusters *WeightedCluster `json:"weighted_clusters,omitempty"`

	Headers     Headers      `json:"headers,omitempty"`
	TimeoutMS   int          `json:"timeout_ms,omitempty"`
	RetryPolicy *RetryPolicy `json:"retry_policy,omitempty"`

	// clusters contains the set of referenced clusters in the route; the field is special
	// and used only to aggregate cluster information after composing routes
	clusters []*Cluster
}

// RetryPolicy definition
// See: https://lyft.github.io/envoy/docs/configuration/http_conn_man/route_config/route.html#retry-policy
type RetryPolicy struct {
	Policy     string `json:"retry_on"` //if unset, set to 5xx,connect-failure,refused-stream
	NumRetries int    `json:"num_retries,omitempty"`
}

// WeightedCluster definition
// See https://lyft.github.io/envoy/docs/configuration/http_conn_man/route_config/route.html
type WeightedCluster struct {
	Clusters         []*WeightedClusterEntry `json:"clusters"`
	RuntimeKeyPrefix string                  `json:"runtime_key_prefix,omitempty"`
}

// WeightedClusterEntry definition. Describes the format of each entry in the WeightedCluster
type WeightedClusterEntry struct {
	Name   string `json:"name"`
	Weight int    `json:"weight"`
}

// VirtualHost definition
type VirtualHost struct {
	Name    string   `json:"name"`
	Domains []string `json:"domains"`
	Routes  []*Route `json:"routes"`
}

// RouteConfig definition
type RouteConfig struct {
	VirtualHosts []*VirtualHost `json:"virtual_hosts"`
}

// Merge operation selects a union of two route configs prioritizing the first.
// It matches virtual hosts by name.
func (rc *RouteConfig) merge(that *RouteConfig) *RouteConfig {
	out := &RouteConfig{}
	set := make(map[string]bool)
	for _, host := range rc.VirtualHosts {
		set[host.Name] = true
		out.VirtualHosts = append(out.VirtualHosts, host)
	}
	for _, host := range that.VirtualHosts {
		if !set[host.Name] {
			out.VirtualHosts = append(out.VirtualHosts, host)
		}
	}
	return out
}

// Clusters aggregates clusters across routes
func (rc *RouteConfig) clusters() []*Cluster {
	out := make([]*Cluster, 0)
	for _, host := range rc.VirtualHosts {
		for _, route := range host.Routes {
			out = append(out, route.clusters...)
		}
	}
	return out
}

// AccessLog definition.
type AccessLog struct {
	Path   string `json:"path"`
	Format string `json:"format,omitempty"`
	Filter string `json:"filter,omitempty"`
}

// NetworkFilterConfig definition
type NetworkFilterConfig struct {
	CodecType         string       `json:"codec_type"`
	StatPrefix        string       `json:"stat_prefix"`
	GenerateRequestID bool         `json:"generate_request_id,omitempty"`
	RouteConfig       *RouteConfig `json:"route_config"`
	Filters           []Filter     `json:"filters"`
	AccessLog         []AccessLog  `json:"access_log"`
	Cluster           string       `json:"cluster,omitempty"`
}

// NetworkFilter definition
type NetworkFilter struct {
	Type   string              `json:"type"`
	Name   string              `json:"name"`
	Config NetworkFilterConfig `json:"config"`
}

// Listener definition
type Listener struct {
	Port           int              `json:"port"`
	Filters        []*NetworkFilter `json:"filters"`
	BindToPort     bool             `json:"bind_to_port"`
	UseOriginalDst bool             `json:"use_original_dst,omitempty"`
}

// RouteConfigs provides routes by virtual host and port
type RouteConfigs map[int]*RouteConfig

// EnsurePort creates a route config if necessary
func (hosts RouteConfigs) EnsurePort(port int) *RouteConfig {
	config, ok := hosts[port]
	if !ok {
		config = &RouteConfig{}
		hosts[port] = config
	}
	return config
}

// Admin definition
type Admin struct {
	AccessLogPath string `json:"access_log_path"`
	Port          int    `json:"port"`
}

// Host definition
type Host struct {
	URL string `json:"url"`
}

// Cluster definition
type Cluster struct {
	Name                     string            `json:"name"`
	ServiceName              string            `json:"service_name,omitempty"`
	ConnectTimeoutMs         int               `json:"connect_timeout_ms"`
	Type                     string            `json:"type"`
	LbType                   string            `json:"lb_type"`
	MaxRequestsPerConnection int               `json:"max_requests_per_connection,omitempty"`
	Hosts                    []Host            `json:"hosts,omitempty"`
	Features                 string            `json:"features,omitempty"`
	CircuitBreaker           *CircuitBreaker   `json:"circuit_breaker,omitempty"`
	OutlierDetection         *OutlierDetection `json:"outlier_detection,omitempty"`

	// special values used by the post-processing passes for outbound clusters
	hostname string
	port     *model.Port
	tag      model.Tag
}

// CircuitBreaker definition
// See: https://lyft.github.io/envoy/docs/configuration/cluster_manager/cluster_circuit_breakers.html#circuit-breakers
type CircuitBreaker struct {
	MaxConnections     int `json:"max_connections,omitempty"`
	MaxPendingRequests int `json:"max_pending_requests,omitempty"`
	MaxRequests        int `json:"max_requests,omitempty"`
	MaxRetries         int `json:"max_retries,omitempty"`
}

// OutlierDetection definition
// See: https://lyft.github.io/envoy/docs/configuration/cluster_manager/cluster_runtime.html#outlier-detection
type OutlierDetection struct {
	ConsecutiveError   int `json:"consecutive_5xx,omitempty"`
	IntervalMS         int `json:"interval_ms,omitempty"`
	BaseEjectionTimeMS int `json:"base_ejection_time_ms,omitempty"`
	MaxEjectionPercent int `json:"max_ejection_percent,omitempty"`
}

// ListenersByPort sorts listeners by port
type ListenersByPort []*Listener

func (l ListenersByPort) Len() int {
	return len(l)
}

func (l ListenersByPort) Swap(i, j int) {
	l[i], l[j] = l[j], l[i]
}

func (l ListenersByPort) Less(i, j int) bool {
	return l[i].Port < l[j].Port
}

// Clusters is a collection of clusters
type Clusters []*Cluster

func (s Clusters) Len() int {
	return len(s)
}

func (s Clusters) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s Clusters) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

// Normalize deduplicates and sorts clusters
func (s Clusters) Normalize() Clusters {
	out := make(Clusters, 0)
	set := make(map[string]bool)
	for _, cluster := range s {
		if !set[cluster.Name] {
			set[cluster.Name] = true
			out = append(out, cluster)
		}
	}
	sort.Sort(out)
	return out
}

// HostsByName sorts clusters by name
type HostsByName []*VirtualHost

func (s HostsByName) Len() int {
	return len(s)
}

func (s HostsByName) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s HostsByName) Less(i, j int) bool {
	return s[i].Name < s[j].Name
}

// RoutesByCluster sorts routes by name
type RoutesByCluster []Route

func (r RoutesByCluster) Len() int {
	return len(r)
}

func (r RoutesByCluster) Swap(i, j int) {
	r[i], r[j] = r[j], r[i]
}

func (r RoutesByCluster) Less(i, j int) bool {
	return r[i].Cluster < r[j].Cluster
}

// Headers sorts headers
type Headers []Header

func (s Headers) Len() int {
	return len(s)
}

func (s Headers) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

func (s Headers) Less(i, j int) bool {
	if s[i].Name == s[j].Name {
		if s[i].Regex == s[j].Regex {
			return s[i].Value < s[j].Value
		}
		// true is less, false is more
		return s[i].Regex
	}
	return s[i].Name < s[j].Name
}

// SDS is a service discovery service definition
type SDS struct {
	Cluster        *Cluster `json:"cluster"`
	RefreshDelayMs int      `json:"refresh_delay_ms"`
}

// ClusterManager definition
type ClusterManager struct {
	Clusters []*Cluster `json:"clusters"`
	SDS      SDS        `json:"sds"`
}

// ByName implements sort
type ByName []Cluster

// Len length
func (a ByName) Len() int {
	return len(a)
}

// Swap elements
func (a ByName) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

// Less compare
func (a ByName) Less(i, j int) bool {
	return a[i].Name < a[j].Name
}

//ByHost implement sort
type ByHost []Host

// Len length
func (a ByHost) Len() int {
	return len(a)
}

// Swap elements
func (a ByHost) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

// Less compare
func (a ByHost) Less(i, j int) bool {
	return a[i].URL < a[j].URL
}
