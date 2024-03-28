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

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/credentials"
	"istio.io/istio/pilot/pkg/features"
	istionetworking "istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/serviceregistry/util/label"
	"istio.io/istio/pilot/pkg/trustbundle"
	networkutil "istio.io/istio/pilot/pkg/util/network"
	"istio.io/istio/pkg/cluster"
	"istio.io/istio/pkg/config/constants"
	"istio.io/istio/pkg/config/host"
	"istio.io/istio/pkg/config/mesh"
	"istio.io/istio/pkg/ledger"
	"istio.io/istio/pkg/maps"
	pm "istio.io/istio/pkg/model"
	"istio.io/istio/pkg/monitoring"
	"istio.io/istio/pkg/network"
	"istio.io/istio/pkg/spiffe"
	"istio.io/istio/pkg/util/identifier"
	netutil "istio.io/istio/pkg/util/net"
	"istio.io/istio/pkg/util/protomarshal"
	"istio.io/istio/pkg/util/sets"
)

type (
	Node                    = pm.Node
	NodeMetadata            = pm.NodeMetadata
	NodeMetaProxyConfig     = pm.NodeMetaProxyConfig
	BootstrapNodeMetadata   = pm.BootstrapNodeMetadata
	TrafficInterceptionMode = pm.TrafficInterceptionMode
	PodPort                 = pm.PodPort
	StringBool              = pm.StringBool
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

// Environment provides an aggregate environmental API for Pilot
type Environment struct {
	// Discovery interface for listing services and instances.
	ServiceDiscovery

	// Config interface for listing routing rules
	ConfigStore

	// Watcher is the watcher for the mesh config (to be merged into the config store)
	mesh.Watcher

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

	ledger ledger.Ledger

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

func (e *Environment) Version() string {
	if x := e.GetLedger(); x != nil {
		return x.RootHash()
	}
	return ""
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

func (e *Environment) GetLedger() ledger.Ledger {
	return e.ledger
}

func (e *Environment) SetLedger(l ledger.Ledger) {
	e.ledger = l
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

func ResourcesToAny(r Resources) []*anypb.Any {
	a := make([]*anypb.Any, 0, len(r))
	for _, rr := range r {
		a = append(a, rr.Resource)
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

// WatchedResource tracks an active DiscoveryRequest subscription.
type WatchedResource struct {
	// TypeUrl is copied from the DiscoveryRequest.TypeUrl that initiated watching this resource.
	// nolint
	TypeUrl string

	// ResourceNames tracks the list of resources that are actively watched.
	// For LDS and CDS, all resources of the TypeUrl type are watched if it is empty.
	// For endpoints the resource names will have list of clusters and for clusters it is empty.
	// For Delta Xds, all resources of the TypeUrl that a client has subscribed to.
	ResourceNames []string

	// Wildcard indicates the subscription is a wildcard subscription. This only applies to types that
	// allow both wildcard and non-wildcard subscriptions.
	Wildcard bool

	// NonceSent is the nonce sent in the last sent response. If it is equal with NonceAcked, the
	// last message has been processed. If empty: we never sent a message of this type.
	NonceSent string

	// NonceAcked is the last acked message.
	NonceAcked string

	// AlwaysRespond, if true, will ensure that even when a request would otherwise be treated as an
	// ACK, it will be responded to. This typically happens when a proxy reconnects to another instance of
	// Istiod. In that case, Envoy expects us to respond to EDS/RDS/SDS requests to finish warming of
	// clusters/listeners.
	// Typically, this should be set to 'false' after response; keeping it true would likely result in an endless loop.
	AlwaysRespond bool

	// LastResources tracks the contents of the last push.
	// This field is extremely expensive to maintain and is typically disabled
	LastResources Resources
}

var istioVersionRegexp = regexp.MustCompile(`^([1-9]+)\.([0-9]+)(\.([0-9]+))?`)

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

// IstioVersion encodes the Istio version of the proxy. This is a low key way to
// do semver style comparisons and generate the appropriate envoy config
type IstioVersion struct {
	Major int
	Minor int
	Patch int
}

var MaxIstioVersion = &IstioVersion{Major: 65535, Minor: 65535, Patch: 65535}

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

// NodeType decides the responsibility of the proxy serves in the mesh
type NodeType string

const (
	// SidecarProxy type is used for sidecar proxies in the application containers
	SidecarProxy NodeType = "sidecar"

	// Router type is used for standalone proxies acting as L7/L4 routers
	Router NodeType = "router"

	// Waypoint type is used for waypoint proxies
	Waypoint NodeType = "waypoint"

	// Ztunnel type is used for node proxies (ztunnel)
	Ztunnel NodeType = "ztunnel"
)

var NodeTypes = [...]NodeType{SidecarProxy, Router, Waypoint, Ztunnel}

// IPMode represents the IP mode of proxy.
type IPMode int

// IPMode constants starting with index 1.
const (
	IPv4 IPMode = iota + 1
	IPv6
	Dual
)

// IsApplicationNodeType verifies that the NodeType is one of the declared constants in the model
func IsApplicationNodeType(nType NodeType) bool {
	switch nType {
	case SidecarProxy, Router, Waypoint, Ztunnel:
		return true
	default:
		return false
	}
}

// ServiceNode encodes the proxy node attributes into a URI-acceptable string
func (node *Proxy) ServiceNode() string {
	ip := ""
	if len(node.IPAddresses) > 0 {
		ip = node.IPAddresses[0]
	}
	return strings.Join([]string{
		string(node.Type), ip, node.ID, node.DNSDomain,
	}, serviceNodeSeparator)
}

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

// SetGatewaysForProxy merges the Gateway objects associated with this
// proxy and caches the merged object in the proxy Node. This is a convenience hack so that
// callers can simply call push.MergedGateways(node) instead of having to
// fetch all the gateways and invoke the merge call in multiple places (lds/rds).
// Must be called after ServiceTargets are set
func (node *Proxy) SetGatewaysForProxy(ps *PushContext) {
	if node.Type != Router {
		return
	}
	node.MergedGateway = ps.mergeGateways(node)
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
	if networkutil.AllIPv4(node.IPAddresses) {
		node.ipMode = IPv4
	} else if networkutil.AllIPv6(node.IPAddresses) {
		node.ipMode = IPv6
	} else {
		node.ipMode = Dual
	}
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

	if !IsApplicationNodeType(NodeType(parts[0])) {
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

// ParsePort extracts port number from a valid proxy address
func ParsePort(addr string) int {
	_, sPort, err := net.SplitHostPort(addr)
	if sPort == "" {
		return 0
	}
	if err != nil {
		log.Warn(err)
	}
	port, pErr := strconv.Atoi(sPort)
	if pErr != nil {
		log.Warn(pErr)
	}

	return port
}

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
func (node *Proxy) CanBindToPort(bindTo bool, port uint32) bool {
	if bindTo {
		if IsPrivilegedPort(port) && node.IsUnprivileged() {
			return false
		}
		if node.Metadata != nil &&
			(node.Metadata.EnvoyPrometheusPort == int(port) || node.Metadata.EnvoyStatusPort == int(port)) {
			// can not bind to port that already bound by proxy static listener
			return false
		}
	}
	return true
}

// IsPrivilegedPort returns true if a given port is in the range 1-1023.
func IsPrivilegedPort(port uint32) bool {
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
	// fall back to get the node name from labels
	// this can happen for an "old" proxy with no `Metadata.NodeName` set
	// TODO: remove this when 1.16 is EOL?
	return node.Labels[label.LabelHostname]
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

func (node *Proxy) EnableHBONE() bool {
	return node.IsAmbient() || (features.EnableHBONE && bool(node.Metadata.EnableHBONE))
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

// CloneWatchedResources clones the watched resources, both the keys and values are shallow copy.
func (node *Proxy) CloneWatchedResources() map[string]*WatchedResource {
	node.RLock()
	defer node.RUnlock()
	return maps.Clone(node.WatchedResources)
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

// SupportsEnvoyExtendedJwt indicates that the proxy JWT extension is capable of
// replacing istio_authn filter.
func (node *Proxy) SupportsEnvoyExtendedJwt() bool {
	return node.IstioVersion == nil ||
		node.IstioVersion.Compare(&IstioVersion{Major: 1, Minor: 21, Patch: -1}) >= 0
}

type GatewayController interface {
	ConfigStoreController
	// Reconcile updates the internal state of the gateway controller for a given input. This should be
	// called before any List/Get calls if the state has changed
	Reconcile(ctx *PushContext) error
	// SecretAllowed determines if a SDS credential is accessible to a given namespace.
	// For example, for resourceName of `kubernetes-gateway://ns-name/secret-name` and namespace of `ingress-ns`,
	// this would return true only if there was a policy allowing `ingress-ns` to access Secrets in the `ns-name` namespace.
	SecretAllowed(resourceName string, namespace string) bool
}

// OutboundListenerClass is a helper to turn a NodeType for outbound to a ListenerClass.
func OutboundListenerClass(t NodeType) istionetworking.ListenerClass {
	if t == Router {
		return istionetworking.ListenerClassGateway
	}
	return istionetworking.ListenerClassSidecarOutbound
}
