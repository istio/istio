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

package model

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	discovery "github.com/envoyproxy/go-control-plane/envoy/service/discovery/v3"
	anypb "google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/structpb"
	"k8s.io/apimachinery/pkg/types"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/features"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/trustbundle"
	networkutil "istio.io/istio/pilot/pkg/util/network"
	v3 "istio.io/istio/pilot/pkg/xds/v3"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/config/mesh/meshwatcher"
	"istio.io/istio/pkg/config/protocol"
	"istio.io/istio/pkg/config/schema/kind"
	"istio.io/istio/pkg/maps"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/identifier"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
	"istio.io/istio/pkg/xds"
)

type (
	Node                    = pm.Node
	NodeMetadata            = pm.NodeMetadata
	NodeMetaProxyConfig     = pm.NodeMetaProxyConfig
	NodeType                = pm.NodeType
	BootstrapNodeMetadata   = pm.BootstrapNodeMetadata
	TrafficInterceptionMode = pm.TrafficInterceptionMode
	PodPort                 = pm.PodPort
	StringBool              = pm.StringBool
	IPMode                  = pm.IPMode
)

const (
	SidecarProxy = pm.SidecarProxy
	Router       = pm.Router
	Waypoint     = pm.Waypoint
	Ztunnel      = pm.Ztunnel

	IPv4 = pm.IPv4
	IPv6 = pm.IPv6
	Dual = pm.Dual
)

var _ mesh.Holder = &Environment{}

func NewEnvironment() *Environment {
	var cache XdsCache
	if features.EnableXDSCaching {
		cache = NewXdsCache()
	} else {
		cache = DisabledCache{}
	}
	return &Environment{
		pushContext:   NewPushContext(),
		Cache:         cache,
		EndpointIndex: NewEndpointIndex(cache),
	}
}

// Watcher is a type alias to keep the embedded type name stable.
type Watcher = meshwatcher.WatcherCollection

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Config interface for listing routing rules
	ConfigStore

	// Watcher is the watcher for the mesh config (to be merged into the config store)
	Watcher

	// NetworksWatcher (loaded from a config map) provides information about the
	// set of networks inside a mesh and how to route to endpoints in each
	// network. Each network provides information about the endpoints in a
	// routable L3 network. A single routable L3 network can have one or more
	// service registries.
	NetworksWatcher mesh.NetworksWatcher

	NetworkManager *NetworkManager

	// mutex used for protecting Environment.pushContext
	mutex sync.RWMutex
	// pushContext holds information during push generation. It is reset on config change, at the beginning
	// of the pushAll. It will hold all errors and stats and possibly caches needed during the entire cache computation.
	// DO NOT USE EXCEPT FOR TESTS AND HANDLING OF NEW CONNECTIONS.
	// ALL USE DURING A PUSH SHOULD USE THE ONE CREATED AT THE
	// START OF THE PUSH, THE GLOBAL ONE MAY CHANGE AND REFLECT A DIFFERENT
	// CONFIG AND PUSH
	pushContext *PushContext

	// DomainSuffix provides a default domain for the Istio server.
	DomainSuffix string

	// TrustBundle: List of Mesh TrustAnchors
	TrustBundle *trustbundle.TrustBundle

	clusterLocalServices ClusterLocalProvider

	CredentialsController credentials.MulticlusterController

	GatewayAPIController GatewayController

	// EndpointShards for a service. This is a global (per-server) list, built from
	// incremental updates. This is keyed by service and namespace
	EndpointIndex *EndpointIndex

	// Cache for XDS resources.
	Cache XdsCache
}

func (e *Environment) Mesh() *meshconfig.MeshConfig {
	if e != nil && e.Watcher != nil {
		return e.Watcher.Mesh()
	}
	return nil
}

func (e *Environment) MeshNetworks() *meshconfig.MeshNetworks {
	if e != nil && e.NetworksWatcher != nil {
		return e.NetworksWatcher.Networks()
	}
	return nil
}

// SetPushContext sets the push context with lock protected
func (e *Environment) SetPushContext(pc *PushContext) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
	e.pushContext = pc
}

// PushContext returns the push context with lock protected
func (e *Environment) PushContext() *PushContext {
	e.mutex.RLock()
	defer e.mutex.RUnlock()
	return e.pushContext
}

// GetDiscoveryAddress parses the DiscoveryAddress specified via MeshConfig.
func (e *Environment) GetDiscoveryAddress() (host.Name, string, error) {
	proxyConfig := mesh.DefaultProxyConfig()
	if e.Mesh().DefaultConfig != nil {
		proxyConfig = e.Mesh().DefaultConfig
	}
	hostname, port, err := net.SplitHostPort(proxyConfig.DiscoveryAddress)
	if err != nil {
		return "", "", fmt.Errorf("invalid Istiod Address: %s, %v", proxyConfig.DiscoveryAddress, err)
	}
	if _, err := strconv.Atoi(port); err != nil {
		return "", "", fmt.Errorf("invalid Istiod Port: %s, %s, %v", port, proxyConfig.DiscoveryAddress, err)
	}
	return host.Name(hostname), port, nil
}

func (e *Environment) AddMeshHandler(h func()) {
	if e != nil && e.Watcher != nil {
		e.Watcher.AddMeshHandler(h)
	}
}

func (e *Environment) AddNetworksHandler(h func()) {
	if e != nil && e.NetworksWatcher != nil {
		e.NetworksWatcher.AddNetworksHandler(h)
	}
}

func (e *Environment) AddMetric(metric monitoring.Metric, key string, proxyID, msg string) {
	if e != nil {
		e.PushContext().AddMetric(metric, key, proxyID, msg)
	}
}

// Init initializes the Environment for use.
func (e *Environment) Init() {
	// Use a default DomainSuffix, if none was provided.
	if len(e.DomainSuffix) == 0 {
		e.DomainSuffix = constants.DefaultClusterLocalDomain
	}

	// Create the cluster-local service registry.
	e.clusterLocalServices = NewClusterLocalProvider(e)
}

func (e *Environment) InitNetworksManager(updater XDSUpdater) (err error) {
	e.NetworkManager, err = NewNetworkManager(e, updater)
	return
}

func (e *Environment) ClusterLocal() ClusterLocalProvider {
	return e.clusterLocalServices
}

func (e *Environment) GetProxyConfigOrDefault(ns string, labels, annotations map[string]string, meshConfig *meshconfig.MeshConfig) *meshconfig.ProxyConfig {
	push := e.PushContext()
	if push != nil && push.ProxyConfigs != nil {
		if generatedProxyConfig := push.ProxyConfigs.EffectiveProxyConfig(
			&NodeMetadata{
				Namespace:   ns,
				Labels:      labels,
				Annotations: annotations,
			}, meshConfig); generatedProxyConfig != nil {
			return generatedProxyConfig
		}
	}
	return mesh.DefaultProxyConfig()
}

// Resources is an alias for array of marshaled resources.
type Resources = []*discovery.Resource

// DeletedResources is an alias for array of strings that represent removed resources in delta.
type DeletedResources = []string

func AnyToUnnamedResources(r []*anypb.Any) Resources {
	a := make(Resources, 0, len(r))
	for _, rr := range r {
		a = append(a, &discovery.Resource{Resource: rr})
	}
	return a
}

// XdsUpdates include information about the subset of updated resources.
// See for example EDS incremental updates.
type XdsUpdates = sets.Set[ConfigKey]

// XdsLogDetails contains additional metadata that is captured by Generators and used by xds processors
// like Ads and Delta to uniformly log.
type XdsLogDetails struct {
	Incremental    bool
	AdditionalInfo string
}

var DefaultXdsLogDetails = XdsLogDetails{}

// XdsResourceGenerator creates the response for a typeURL DiscoveryRequest or DeltaDiscoveryRequest. If no generator
// is associated with a Proxy, the default (a networking.core.ConfigGenerator instance) will be used.
// The server may associate a different generator based on client metadata. Different
// WatchedResources may use same or different Generator.
// Note: any errors returned will completely close the XDS stream. Use with caution; typically and empty
// or no response is preferred.
type XdsResourceGenerator interface {
	// Generate generates the Sotw resources for Xds.
	Generate(proxy *Proxy, w *WatchedResource, req *PushRequest) (Resources, XdsLogDetails, error)
}

// XdsDeltaResourceGenerator generates Sotw and delta resources.
type XdsDeltaResourceGenerator interface {
	XdsResourceGenerator
	// GenerateDeltas returns the changed and removed resources, along with whether or not delta was actually used.
	GenerateDeltas(proxy *Proxy, req *PushRequest, w *WatchedResource) (Resources, DeletedResources, XdsLogDetails, bool, error)
}

// Proxy contains information about an specific instance of a proxy (envoy sidecar, gateway,
// etc). The Proxy is initialized when a sidecar connects to Pilot, and populated from
// 'node' info in the protocol as well as data extracted from registries.
//
// In current Istio implementation nodes use a 4-parts '~' delimited ID.
// Type~IPAddress~ID~Domain
type Proxy struct {
	sync.RWMutex

	// Type specifies the node type. First part of the ID.
	Type NodeType

	// IPAddresses is the IP addresses of the proxy used to identify it and its
	// co-located service instances. Example: "10.60.1.6". In some cases, the host
	// where the proxy and service instances reside may have more than one IP address
	IPAddresses []string

	// ID is the unique platform-specific sidecar proxy ID. For k8s it is the pod ID and
	// namespace <podName.namespace>.
	ID string

	// Locality is the location of where Envoy proxy runs. This is extracted from
	// the registry where possible. If the registry doesn't provide a locality for the
	// proxy it will use the one sent via ADS that can be configured in the Envoy bootstrap
	Locality *core.Locality

	// DNSDomain defines the DNS domain suffix for short hostnames (e.g.
	// "default.svc.cluster.local")
	DNSDomain string

	// ConfigNamespace defines the namespace where this proxy resides
	// for the purposes of network scoping.
	// NOTE: DO NOT USE THIS FIELD TO CONSTRUCT DNS NAMES
	ConfigNamespace string

	// Labels specifies the set of workload instance (ex: k8s pod) labels associated with this node.
	// Labels can be different from that in Metadata because of pod labels update after startup,
	// while NodeMetadata.Labels are set during bootstrap.
	Labels map[string]string

	// Metadata key-value pairs extending the Node identifier
	Metadata *NodeMetadata

	// the sidecarScope associated with the proxy
	SidecarScope *SidecarScope

	// the sidecarScope associated with the proxy previously
	PrevSidecarScope *SidecarScope

	// The merged gateways associated with the proxy if this is a Router
	MergedGateway *MergedGateway

	// PrevMergedGateway contains information about merged gateway associated with the proxy previously
	PrevMergedGateway *PrevMergedGateway

	// ServiceTargets contains a list of all Services associated with the proxy, contextualized for this particular proxy.
	// These are unique to this proxy, as the port information is specific to it - while a ServicePort is shared with the
	// service, the target port may be distinct per-endpoint. So this maintains a view specific to this proxy.
	// ServiceTargets will maintain a list entry for each Service-port, so if we have 2 services each with 3 ports, we
	// would have 6 entries.
	ServiceTargets []ServiceTarget

	// Istio version associated with the Proxy
	IstioVersion *IstioVersion

	// VerifiedIdentity determines whether a proxy had its identity verified. This
	// generally occurs by JWT or mTLS authentication. This can be false when
	// connecting over plaintext. If this is set to true, we can verify the proxy has
	// access to ConfigNamespace namespace. However, other options such as node type
	// are not part of an Istio identity and thus are not verified.
	VerifiedIdentity *spiffe.Identity

	// IPMode of proxy.
	ipMode IPMode

	// GlobalUnicastIP stores the global unicast IP if available, otherwise nil
	GlobalUnicastIP string

	// XdsResourceGenerator is used to generate resources for the node, based on the PushContext.
	// If nil, the default networking/core v2 generator is used. This field can be set
	// at connect time, based on node metadata, to trigger generation of a different style
	// of configuration.
	XdsResourceGenerator XdsResourceGenerator

	// WatchedResources contains the list of watched resources for the proxy, keyed by the DiscoveryRequest TypeUrl.
	WatchedResources map[string]*WatchedResource

	// XdsNode is the xDS node identifier
	XdsNode *core.Node

	workloadEntryName        string
	workloadEntryAutoCreated bool

	// LastPushContext stores the most recent push context for this proxy. This will be monotonically
	// increasing in version. Requests should send config based on this context; not the global latest.
	// Historically, the latest was used which can cause problems when computing whether a push is
	// required, as the computed sidecar scope version would not monotonically increase.
	LastPushContext *PushContext
	// LastPushTime records the time of the last push. This is used in conjunction with
	// LastPushContext; the XDS cache depends on knowing the time of the PushContext to determine if a
	// key is stale or not.
	LastPushTime time.Time
}

type WatchedResource = xds.WatchedResource

// GetView returns a restricted view of the mesh for this proxy. The view can be
// restricted by network (via ISTIO_META_REQUESTED_NETWORK_VIEW).
// If not set, we assume that the proxy wants to see endpoints in any network.
func (node *Proxy) GetView() ProxyView {
	return newProxyView(node)
}

// InNetwork returns true if the proxy is on the given network, or if either
// the proxy's network or the given network is unspecified ("").
func (node *Proxy) InNetwork(network network.ID) bool {
	return node == nil || identifier.IsSameOrEmpty(network.String(), node.Metadata.Network.String())
}

// InCluster returns true if the proxy is in the given cluster, or if either
// the proxy's cluster id or the given cluster id is unspecified ("").
func (node *Proxy) InCluster(cluster cluster.ID) bool {
	return node == nil || identifier.IsSameOrEmpty(cluster.String(), node.Metadata.ClusterID.String())
}

// IsWaypointProxy returns true if the proxy is acting as a waypoint proxy in an ambient mesh.
func (node *Proxy) IsWaypointProxy() bool {
	return node.Type == Waypoint
}

// IsZTunnel returns true if the proxy is acting as a ztunnel in an ambient mesh.
func (node *Proxy) IsZTunnel() bool {
	return node.Type == Ztunnel
}

// IsAmbient returns true if the proxy is acting as either a ztunnel or a waypoint proxy in an ambient mesh.
func (node *Proxy) IsAmbient() bool {
	return node.IsWaypointProxy() || node.IsZTunnel()
}

var NodeTypes = [...]NodeType{SidecarProxy, Router, Waypoint, Ztunnel}

// SetSidecarScope identifies the sidecar scope object associated with this
// proxy and updates the proxy Node. This is a convenience hack so that
// callers can simply call push.Services(node) while the implementation of
// push.Services can return the set of services from the proxyNode's
// sidecar scope or from the push context's set of global services. Similar
// logic applies to push.VirtualServices and push.DestinationRule. The
// short cut here is useful only for CDS and parts of RDS generation code.
//
// Listener generation code will still use the SidecarScope object directly
// as it needs the set of services for each listener port.
func (node *Proxy) SetSidecarScope(ps *PushContext) {
	sidecarScope := node.SidecarScope

	switch node.Type {
	case SidecarProxy:
		node.SidecarScope = ps.getSidecarScope(node, node.Labels)
	case Router, Waypoint:
		// Gateways should just have a default scope with egress: */*
		node.SidecarScope = ps.getSidecarScope(node, nil)
	}
	node.PrevSidecarScope = sidecarScope
}

var istioVersionRegexp = regexp.MustCompile(`^([1-9]+)\.([0-9]+)(\.([0-9]+))?`)

// IstioVersion encodes the Istio version of the proxy. This is a low key way to
// do semver style comparisons and generate the appropriate envoy config
type IstioVersion struct {
	Major int
	Minor int
	Patch int
}

var MaxIstioVersion = &IstioVersion{Major: 65535, Minor: 65535, Patch: 65535}

// ParseIstioVersion parses a version string and returns IstioVersion struct
func ParseIstioVersion(ver string) *IstioVersion {
	// strip the release- prefix if any and extract the version string
	ver = istioVersionRegexp.FindString(strings.TrimPrefix(ver, "release-"))

	if ver == "" {
		// return very large values assuming latest version
		return MaxIstioVersion
	}

	parts := strings.Split(ver, ".")
	// we are guaranteed to have at least major and minor based on the regex
	major, _ := strconv.Atoi(parts[0])
	minor, _ := strconv.Atoi(parts[1])
	// Assume very large patch release if not set
	patch := 65535
	if len(parts) > 2 {
		patch, _ = strconv.Atoi(parts[2])
	}
	return &IstioVersion{Major: major, Minor: minor, Patch: patch}
}

func (pversion *IstioVersion) String() string {
	return fmt.Sprintf("%d.%d.%d", pversion.Major, pversion.Minor, pversion.Patch)
}

// Compare returns -1/0/1 if version is less than, equal or greater than inv
// To compare only on major, call this function with { X, -1, -1}.
// to compare only on major & minor, call this function with {X, Y, -1}.
func (pversion *IstioVersion) Compare(inv *IstioVersion) int {
	// check major
	if r := compareVersion(pversion.Major, inv.Major); r != 0 {
		return r
	}

	// check minor
	if inv.Minor > -1 {
		if r := compareVersion(pversion.Minor, inv.Minor); r != 0 {
			return r
		}

		// check patch
		if inv.Patch > -1 {
			if r := compareVersion(pversion.Patch, inv.Patch); r != 0 {
				return r
			}
		}
	}
	return 0
}

func compareVersion(ov, nv int) int {
	if ov == nv {
		return 0
	}
	if ov < nv {
		return -1
	}
	return 1
}

func (node *Proxy) VersionGreaterOrEqual(inv *IstioVersion) bool {
	if inv == nil {
		return true
	}
	return node.IstioVersion.Compare(inv) >= 0
}

// SetGatewaysForProxy merges the Gateway objects associated with this
// proxy and caches the merged object in the proxy Node. This is a convenience hack so that
// callers can simply call push.MergedGateways(node) instead of having to
// fetch all the gateways and invoke the merge call in multiple places (lds/rds).
// Must be called after ServiceTargets are set
func (node *Proxy) SetGatewaysForProxy(ps *PushContext) {
	if node.Type != Router {
		return
	}
	var prevMergedGateway MergedGateway
	if node.MergedGateway != nil {
		prevMergedGateway = *node.MergedGateway
	}
	node.MergedGateway = ps.mergeGateways(node)
	node.PrevMergedGateway = &PrevMergedGateway{
		ContainsAutoPassthroughGateways: prevMergedGateway.ContainsAutoPassthroughGateways,
		AutoPassthroughSNIHosts:         prevMergedGateway.GetAutoPassthroughGatewaySNIHosts(),
	}
}

func (node *Proxy) ShouldUpdateServiceTargets(updates sets.Set[ConfigKey]) bool {
	// we only care for services which can actually select this proxy
	for config := range updates {
		if config.Kind == kind.ServiceEntry || config.Namespace == node.Metadata.Namespace {
			return true
		}
	}

	return false
}

func (node *Proxy) SetServiceTargets(serviceDiscovery ServiceDiscovery) {
	instances := serviceDiscovery.GetProxyServiceTargets(node)

	// Keep service instances in order of creation/hostname.
	sort.SliceStable(instances, func(i, j int) bool {
		if instances[i].Service != nil && instances[j].Service != nil {
			if !instances[i].Service.CreationTime.Equal(instances[j].Service.CreationTime) {
				return instances[i].Service.CreationTime.Before(instances[j].Service.CreationTime)
			}
			// Additionally, sort by hostname just in case services created automatically at the same second.
			return instances[i].Service.Hostname < instances[j].Service.Hostname
		}
		return true
	})

	node.ServiceTargets = instances
}

// SetWorkloadLabels will set the node.Labels.
// It merges both node meta labels and workload labels and give preference to workload labels.
func (node *Proxy) SetWorkloadLabels(env *Environment) {
	// If this is VM proxy, do not override labels at all, because in istio test we use pod to simulate VM.
	if node.IsVM() {
		node.Labels = node.Metadata.Labels
		return
	}
	labels := env.GetProxyWorkloadLabels(node)
	if labels != nil {
		node.Labels = make(map[string]string, len(labels)+len(node.Metadata.StaticLabels))
		// we can't just equate proxy workload labels to node meta labels as it may be customized by user
		// with `ISTIO_METAJSON_LABELS` env (pkg/bootstrap/config.go extractAttributesMetadata).
		// so, we fill the `ISTIO_METAJSON_LABELS` as well.
		for k, v := range node.Metadata.StaticLabels {
			node.Labels[k] = v
		}
		for k, v := range labels {
			node.Labels[k] = v
		}
	} else {
		// If could not find pod labels, fallback to use the node metadata labels.
		node.Labels = node.Metadata.Labels
	}
}

// DiscoverIPMode discovers the IP Versions supported by Proxy based on its IP addresses.
func (node *Proxy) DiscoverIPMode() {
	node.ipMode = pm.DiscoverIPMode(node.IPAddresses)
	node.GlobalUnicastIP = networkutil.GlobalUnicastIP(node.IPAddresses)
}

// SupportsIPv4 returns true if proxy supports IPv4 addresses.
func (node *Proxy) SupportsIPv4() bool {
	return node.ipMode == IPv4 || node.ipMode == Dual
}

// SupportsIPv6 returns true if proxy supports IPv6 addresses.
func (node *Proxy) SupportsIPv6() bool {
	return node.ipMode == IPv6 || node.ipMode == Dual
}

// IsIPv6 returns true if proxy only supports IPv6 addresses.
func (node *Proxy) IsIPv6() bool {
	return node.ipMode == IPv6
}

func (node *Proxy) IsDualStack() bool {
	return node.ipMode == Dual
}

// GetIPMode returns proxy's ipMode
func (node *Proxy) GetIPMode() IPMode {
	return node.ipMode
}

// SetIPMode set node's ip mode
// Note: Donot use this function directly in most cases, use DiscoverIPMode instead.
func (node *Proxy) SetIPMode(mode IPMode) {
	node.ipMode = mode
}

// ParseMetadata parses the opaque Metadata from an Envoy Node into string key-value pairs.
// Any non-string values are ignored.
func ParseMetadata(metadata *structpb.Struct) (*NodeMetadata, error) {
	if metadata == nil {
		return &NodeMetadata{}, nil
	}

	bootstrapNodeMeta, err := ParseBootstrapNodeMetadata(metadata)
	if err != nil {
		return nil, err
	}
	return &bootstrapNodeMeta.NodeMetadata, nil
}

// ParseBootstrapNodeMetadata parses the opaque Metadata from an Envoy Node into string key-value pairs.
func ParseBootstrapNodeMetadata(metadata *structpb.Struct) (*BootstrapNodeMetadata, error) {
	if metadata == nil {
		return &BootstrapNodeMetadata{}, nil
	}

	b, err := protomarshal.MarshalProtoNames(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to read node metadata %v: %v", metadata, err)
	}
	meta := &BootstrapNodeMetadata{}
	if err := json.Unmarshal(b, meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal node metadata (%v): %v", string(b), err)
	}
	return meta, nil
}

// ParseServiceNodeWithMetadata parse the Envoy Node from the string generated by ServiceNode
// function and the metadata.
func ParseServiceNodeWithMetadata(nodeID string, metadata *NodeMetadata) (*Proxy, error) {
	parts := strings.Split(nodeID, serviceNodeSeparator)
	out := &Proxy{
		Metadata: metadata,
	}

	if len(parts) != 4 {
		return out, fmt.Errorf("missing parts in the service node %q", nodeID)
	}

	if !pm.IsApplicationNodeType(NodeType(parts[0])) {
		return out, fmt.Errorf("invalid node type (valid types: %v) in the service node %q", NodeTypes, nodeID)
	}
	out.Type = NodeType(parts[0])

	// Get all IP Addresses from Metadata
	if hasValidIPAddresses(metadata.InstanceIPs) {
		out.IPAddresses = metadata.InstanceIPs
	} else if netutil.IsValidIPAddress(parts[1]) {
		// Fall back, use IP from node id, it's only for backward-compatibility, IP should come from metadata
		out.IPAddresses = append(out.IPAddresses, parts[1])
	}

	// Does query from ingress or router have to carry valid IP address?
	if len(out.IPAddresses) == 0 {
		return out, fmt.Errorf("no valid IP address in the service node id or metadata")
	}

	out.ID = parts[2]
	out.DNSDomain = parts[3]
	if len(metadata.IstioVersion) == 0 {
		log.Warnf("Istio Version is not found in metadata for %v, which may have undesirable side effects", out.ID)
	}
	out.IstioVersion = ParseIstioVersion(metadata.IstioVersion)
	return out, nil
}

// GetOrDefault returns either the value, or the default if the value is empty. Useful when retrieving node metadata fields.
func GetOrDefault(s string, def string) string {
	return pm.GetOrDefault(s, def)
}

// GetProxyConfigNamespace extracts the namespace associated with the proxy
// from the proxy metadata or the proxy ID
func GetProxyConfigNamespace(proxy *Proxy) string {
	if proxy == nil {
		return ""
	}

	// First look for ISTIO_META_CONFIG_NAMESPACE
	// All newer proxies (from Istio 1.1 onwards) are supposed to supply this
	if len(proxy.Metadata.Namespace) > 0 {
		return proxy.Metadata.Namespace
	}

	// if not found, for backward compatibility, extract the namespace from
	// the proxy domain. this is a k8s specific hack and should be enabled
	parts := strings.Split(proxy.DNSDomain, ".")
	if len(parts) > 1 { // k8s will have namespace.<domain>
		return parts[0]
	}

	return ""
}

const (
	serviceNodeSeparator = "~"
)

// hasValidIPAddresses returns true if the input ips are all valid, otherwise returns false.
func hasValidIPAddresses(ipAddresses []string) bool {
	if len(ipAddresses) == 0 {
		return false
	}
	for _, ipAddress := range ipAddresses {
		if !netutil.IsValidIPAddress(ipAddress) {
			return false
		}
	}
	return true
}

const (
	// InterceptionNone indicates that the workload is not using IPtables for traffic interception
	InterceptionNone TrafficInterceptionMode = "NONE"

	// InterceptionTproxy implies traffic intercepted by IPtables with TPROXY mode
	InterceptionTproxy TrafficInterceptionMode = "TPROXY"

	// InterceptionRedirect implies traffic intercepted by IPtables with REDIRECT mode
	// This is our default mode
	InterceptionRedirect TrafficInterceptionMode = "REDIRECT"
)

// GetInterceptionMode extracts the interception mode associated with the proxy
// from the proxy metadata
func (node *Proxy) GetInterceptionMode() TrafficInterceptionMode {
	if node == nil {
		return InterceptionRedirect
	}

	switch node.Metadata.InterceptionMode {
	case "TPROXY":
		return InterceptionTproxy
	case "REDIRECT":
		return InterceptionRedirect
	case "NONE":
		return InterceptionNone
	}

	return InterceptionRedirect
}

// IsUnprivileged returns true if the proxy has explicitly indicated that it is
// unprivileged, i.e. it cannot bind to the privileged ports 1-1023.
func (node *Proxy) IsUnprivileged() bool {
	if node == nil || node.Metadata == nil {
		return false
	}
	// expect explicit "true" value
	unprivileged, _ := strconv.ParseBool(node.Metadata.UnprivilegedPod)
	return unprivileged
}

// CanBindToPort returns true if the proxy can bind to a given port.
// canbind indicates whether the proxy can bind to the port.
// knownlistener indicates whether the check failed if the proxy is trying to bind to a port that is reserved for a static listener or virtual listener.
func (node *Proxy) CanBindToPort(bindTo bool, proxy *Proxy, push *PushContext,
	bind string, port int, protocol protocol.Instance, wildcard string,
) (canbind bool, knownlistener bool) {
	if bindTo {
		if isPrivilegedPort(port) && node.IsUnprivileged() {
			return false, false
		}
	}
	if conflictWithReservedListener(proxy, push, bind, port, protocol, wildcard) {
		return false, true
	}
	return true, false
}

// conflictWithReservedListener checks whether the listener address bind:port conflicts with
// - static listener port：default is 15021 and 15090
// - virtual listener port: default is 15001 and 15006 (only need to check for outbound listener)
func conflictWithReservedListener(proxy *Proxy, push *PushContext, bind string, port int, protocol protocol.Instance, wildcard string) bool {
	if bind != "" {
		if bind != wildcard {
			return false
		}
	} else if !protocol.IsHTTP() {
		// if the protocol is HTTP and bind == "", the listener address will be 0.0.0.0:port
		return false
	}

	var conflictWithStaticListener, conflictWithVirtualListener bool

	// bind == wildcard
	// or bind unspecified, but protocol is HTTP
	if proxy.Metadata != nil {
		conflictWithStaticListener = proxy.Metadata.EnvoyStatusPort == port || proxy.Metadata.EnvoyPrometheusPort == port
	}
	if push != nil {
		conflictWithVirtualListener = int(push.Mesh.ProxyListenPort) == port || int(push.Mesh.ProxyInboundListenPort) == port
	}
	return conflictWithStaticListener || conflictWithVirtualListener
}

// isPrivilegedPort returns true if a given port is in the range 1-1023.
func isPrivilegedPort(port int) bool {
	// check for 0 is important because:
	// 1) technically, 0 is not a privileged port; any process can ask to bind to 0
	// 2) this function will be receiving 0 on input in the case of UDS listeners
	return 0 < port && port < 1024
}

func (node *Proxy) IsVM() bool {
	// TODO use node metadata to indicate that this is a VM instead of the TestVMLabel
	return node.Metadata.Labels[constants.TestVMLabel] != ""
}

func (node *Proxy) IsProxylessGrpc() bool {
	return node.Metadata != nil && node.Metadata.Generator == "grpc"
}

func (node *Proxy) GetNodeName() string {
	if node.Metadata != nil && len(node.Metadata.NodeName) > 0 {
		return node.Metadata.NodeName
	}
	return ""
}

func (node *Proxy) GetClusterID() cluster.ID {
	if node == nil || node.Metadata == nil {
		return ""
	}
	return node.Metadata.ClusterID
}

func (node *Proxy) GetNamespace() string {
	if node == nil || node.Metadata == nil {
		return ""
	}
	return node.Metadata.Namespace
}

func (node *Proxy) GetIstioVersion() string {
	if node == nil || node.Metadata == nil {
		return ""
	}
	return node.Metadata.IstioVersion
}

func (node *Proxy) GetID() string {
	if node == nil {
		return ""
	}
	return node.ID
}

func (node *Proxy) FuzzValidate() bool {
	if node.Metadata == nil {
		return false
	}
	found := false
	for _, t := range NodeTypes {
		if node.Type == t {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	return len(node.IPAddresses) != 0
}

func (node *Proxy) EnableHBONEListen() bool {
	return node.IsAmbient() || (features.EnableSidecarHBONEListening && bool(node.Metadata.EnableHBONE))
}

func (node *Proxy) EnableListenFromAmbientEastWestGateway() bool {
	return !node.IsAmbient() && bool(node.Metadata.ListenFromAmbientEastWestGateway)
}

func (node *Proxy) SetWorkloadEntry(name string, create bool) {
	node.Lock()
	defer node.Unlock()
	node.workloadEntryName = name
	node.workloadEntryAutoCreated = create
}

func (node *Proxy) WorkloadEntry() (string, bool) {
	node.RLock()
	defer node.RUnlock()
	return node.workloadEntryName, node.workloadEntryAutoCreated
}

// ShallowCloneWatchedResources clones the watched resources, both the keys and values are shallow copy.
func (node *Proxy) ShallowCloneWatchedResources() map[string]*WatchedResource {
	node.RLock()
	defer node.RUnlock()
	return maps.Clone(node.WatchedResources)
}

// DeepCloneWatchedResources clones the watched resources
func (node *Proxy) DeepCloneWatchedResources() map[string]WatchedResource {
	node.RLock()
	defer node.RUnlock()
	m := make(map[string]WatchedResource, len(node.WatchedResources))
	for k, v := range node.WatchedResources {
		m[k] = *v
	}
	return m
}

func (node *Proxy) GetWatchedResourceTypes() sets.String {
	node.RLock()
	defer node.RUnlock()

	ret := sets.NewWithLength[string](len(node.WatchedResources))
	for typeURL := range node.WatchedResources {
		ret.Insert(typeURL)
	}
	return ret
}

func (node *Proxy) GetWatchedResource(typeURL string) *WatchedResource {
	node.RLock()
	defer node.RUnlock()

	return node.WatchedResources[typeURL]
}

func (node *Proxy) NonceSent(typeURL string) string {
	node.RLock()
	defer node.RUnlock()

	wr := node.WatchedResources[typeURL]
	if wr != nil {
		return wr.NonceSent
	}
	return ""
}

func (node *Proxy) NonceAcked(typeURL string) string {
	node.RLock()
	defer node.RUnlock()

	wr := node.WatchedResources[typeURL]
	if wr != nil {
		return wr.NonceAcked
	}
	return ""
}

func (node *Proxy) Clusters() []string {
	node.RLock()
	defer node.RUnlock()
	wr := node.WatchedResources[v3.EndpointType]
	if wr != nil {
		return wr.ResourceNames.UnsortedList()
	}
	return nil
}

func (node *Proxy) NewWatchedResource(typeURL string, names []string) {
	node.Lock()
	defer node.Unlock()

	node.WatchedResources[typeURL] = &WatchedResource{TypeUrl: typeURL, ResourceNames: sets.New(names...)}
	// For all EDS requests that we have already responded with in the same stream let us
	// force the response. It is important to respond to those requests for Envoy to finish
	// warming of those resources(Clusters).
	// This can happen with the following sequence
	// 1. Envoy disconnects and reconnects to Istiod.
	// 2. Envoy sends EDS request and we respond with it.
	// 3. Envoy sends CDS request and we respond with clusters.
	// 4. Envoy detects a change in cluster state and tries to warm those clusters and send EDS request for them.
	// 5. We should respond to the EDS request with Endpoints to let Envoy finish cluster warming.
	// Refer to https://github.com/envoyproxy/envoy/issues/13009 for more details.
	for _, dependent := range WarmingDependencies(typeURL) {
		if dwr, exists := node.WatchedResources[dependent]; exists {
			dwr.AlwaysRespond = true
		}
	}
}

// WarmingDependencies returns the dependent typeURLs that need to be responded with
// for warming of this typeURL.
func WarmingDependencies(typeURL string) []string {
	switch typeURL {
	case v3.ClusterType:
		return []string{v3.EndpointType}
	default:
		return nil
	}
}

func (node *Proxy) AddOrUpdateWatchedResource(r *WatchedResource) {
	if r == nil {
		return
	}
	node.Lock()
	defer node.Unlock()
	node.WatchedResources[r.TypeUrl] = r
}

func (node *Proxy) UpdateWatchedResource(typeURL string, updateFn func(*WatchedResource) *WatchedResource) {
	node.Lock()
	defer node.Unlock()
	r := node.WatchedResources[typeURL]
	r = updateFn(r)
	if r != nil {
		node.WatchedResources[typeURL] = r
	} else {
		delete(node.WatchedResources, typeURL)
	}
}

func (node *Proxy) DeleteWatchedResource(typeURL string) {
	node.Lock()
	defer node.Unlock()

	delete(node.WatchedResources, typeURL)
}

type InferenceGatewayContext interface {
	// HasInferencePool returns whether or not a given gateway has a reference to an InferencePool
	HasInferencePool(types.NamespacedName) bool
}

type GatewayController interface {
	ConfigStoreController
	InferenceGatewayContext
	// Reconcile updates the internal state of the gateway controller for a given input. This should be
	// called before any List/Get calls if the state has changed
	Reconcile(ctx *PushContext)
	// SecretAllowed determines if a SDS credential is accessible to a given namespace.
	// For example, for resourceName of `kubernetes-gateway://ns-name/secret-name` and namespace of `ingress-ns`,
	// this would return true only if there was a policy allowing `ingress-ns` to access Secrets in the `ns-name` namespace.
	SecretAllowed(ourKind config.GroupVersionKind, resourceName string, namespace string) bool
}

// OutboundListenerClass is a helper to turn a NodeType for outbound to a ListenerClass.
func OutboundListenerClass(t NodeType) istionetworking.ListenerClass {
	if t == Router {
		return istionetworking.ListenerClassGateway
	}
	return istionetworking.ListenerClassSidecarOutbound
}
