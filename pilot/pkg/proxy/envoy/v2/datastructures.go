// Copyright 2018 Istio Authors
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
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	types "github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	route "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

const (
	// TODO: move istioConfigFilter to github.com/istio.io/api
	// The internal filter name for attributes that should be used for
	// constructing Istio attributes on the Endpoint in reverse DNS
	// form. See xdsapi.Metadata.Name
	istioConfigFilter = "io.istio.istio-config"

	// nameValueSeparator is a separator for creating label name-value keys used for
	// reverse lookups on labels.
	nameValueSeparator = "\x1F"

	// RuleSubsetSeparator separates the destination rule name and the subset name for
	// the key to the subsetEndpoints map.
	RuleSubsetSeparator = "|"

	dns1123LabelFmt string = "[a-z0-9]([-a-z0-9]*[a-z0-9])?"

	// a wild-card prefix is an '*', a normal DNS1123 label with a leading '*' or '*-', or a normal DNS1123 label
	wildcardPrefix string = `\*|(\*|\*-)?(` + dns1123LabelFmt + `)`
)

// DestinationRuleType is an enumeration for how route.DestinationRule.Name
// should be interpreted, i.e. service domain, short name, CIDR, etc...
// TODO: move all DestinationAttributes to github.com/istio.io/api
// Key Istio attribute names for mapping endpoints to subsets.
// For details, please see
// https://istio.io/docs/reference/config/mixer/attribute-vocabulary.html
type DestinationRuleType int

const (
	// DestinationUID is the attribute name for Mesh unique, environment-specific
	// unique identifier for the server instance of the destination service. Mesh
	// uses the label to uqniquely identify the endpoint when it receives updates
	// from the service registry. Example: kubernetes://my-svc-234443-5sffe.my-namespace
	// No two instances running anywhere in Mesh can have the same value for
	// UID.
	DestinationUID DestinationAttribute = "destination.uid"

	// DestinationService represents the fully qualified name of the service that the server
	// belongs to. Example: "my-svc.my-namespace.svc.cluster.local"
	DestinationService DestinationAttribute = "destination.service"

	// DestinationName represents the short name of the service that the server
	// belongs to. Example: "my-svc"
	DestinationName DestinationAttribute = "destination.name"

	// DestinationNamespace represents the namespace of the service. Example: "default"
	DestinationNamespace DestinationAttribute = "destination.namespace"

	// DestinationDomain represents the domain portion of the service name, excluding
	// the name and namespace, example: svc.cluster.local
	DestinationDomain DestinationAttribute = "destination.domain"

	// DestinationIP represents the IP address of the server instance, example 10.0.0.104.
	// This IP is expected to be reachable from Pilot. No distinction
	// is being made for directly reachable service instances versus those
	// behind a VIP. Istio's health discovery service will ensure that this
	// endpoint's capacity is correctly reported accordingly.
	DestinationIP DestinationAttribute = "destination.ip"

	// DestinationPort represents the recipient port on the server IP address, Example: 443
	DestinationPort DestinationAttribute = "destination.port"

	// DestinationUser represents the user running the destination application, example:
	// my-workload-identity
	DestinationUser DestinationAttribute = "destination.user"

	// DestinationProtocol represents the protocol of the connection being proxied, example:
	// grpc
	DestinationProtocol DestinationAttribute = "context.protocol"
)

// ConfigChangeType is an enumeration for config changes, i.e add, update, delete
type ConfigChangeType int

// Enumerated constants for ConfigChangeType to indicate that
// the associated config data is being added, updated or deleted.
// The association implicitly expects the associated config data
// to furnish some form of unique identification so that this
// configuration element is updated independently of all other
// configuration elements within Pilot.
const (
	ConfigAdd ConfigChangeType = iota
	ConfigUpdate
	ConfigDelete
)

// SocketProtocol identifies the type of IP protocol, i.e. TCP/UDP
type SocketProtocol int

// Enumerated constants for SocketProtocol that's coupled with
// the internal implementation xdsapi.LbEndpoint
const (
	SocketProtocolTCP SocketProtocol = iota
	SocketProtocolUDP
)

// Enumerated constants for DestinationRuleType based on how route.DestinationRule.Name
// should be interpreted.
// TODO: Move DestinationRuleType to github.com/istio.io/api
const (
	// DestinationRuleService is a type of destination rule where the
	// rule name is an FQDN of the service and resulting Subsets ought
	// to be further scoped to this FQDN.
	DestinationRuleService DestinationRuleType = iota

	// DestinationRuleName is a type of destination rule where
	// the rule name is the short name of the service and the
	// resulting subset ought to be further scoped to only those
	// Endpoints whose short service name match this short name.
	DestinationRuleName

	// DestinationRuleIP is a type of destination rule where
	// the rule name is a specific IP and the resulting subset
	// ought to be further scoped to this Endpoint's IP to determine
	// the Subset.
	DestinationRuleIP

	// DestinationRuleWildcard is a type of destination rule where
	// the rule name is a wild card domain name and the
	// resulting subset ought to be further scoped to only those
	// Endpoints whose domains match this wild card domain.
	DestinationRuleWildcard

	// DestinationRuleCIDR is a type of destination rule where
	// the rule name is a CIDR and the resulting subset
	// ought to be further scoped to only those Endpoints whose
	// IPs that match this CIDR.
	DestinationRuleCIDR
)

var (
	// prohibitedAttrSet contains labels that should not be supplied
	// to NewEndpoint(.... labels)
	prohibitedAttrSet = map[string]bool{
		DestinationIP.AttrName():   true,
		DestinationPort.AttrName(): true,
	}
	// Compiled regex for comparing wildcard prefixes.
	wildcardDomainRegex = regexp.MustCompile(wildcardPrefix)
)

// DestinationAttribute encapsulates enums for key Istio attribute names used in Subsets
// and Endpoints
type DestinationAttribute string

// Endpoints (react to updates in registry endpoints) (notify clusters)
// 1. Endpoints can be shared across multiple clusters.
// 2. The membership of an endpoint in a cluster is based on the set of labels
//    associated with the endpoint and the set of label selectors associated with the cluster.
// 3. When a new endpoint is added, the label selectors associated with the clusters have
//    to be evaluated to determine the clusters to which the endpoint belongs to.
// 4. When an endpoint is deleted, it needs to be removed from all associated clusters.
// 5. When one or more labels are added to an endpoint, the label selectors from clusters
//    that "do not have this endpoint" need to be reevaluated to see if the newly added labels
//    would cause this endpoint to be added to these clusters.
// 6. When one or more labels are "removed" from an endpoint, the clusters currently associated
//    with the endpoint need to be reevaluated to see if the endpoint still qualifies to be a member
//    of these clusters.

// Clusters (react to updates in services, dest rules, ext services)
// 1. Every service (registry defined, external) in the system has a corresponding cluster
// 2. Every subset expressed in a DestinationRule will have an associated cluster.
// 3. Clusters are added/removed when a service is added/removed or when subsets are
//    added/removed from DestinationRule or when a service is updated (ports are added/removed)
// 4. Clusters need to be updated when new settings (e.g., circuit breakers, auth) are added
//    via DestinationRules.

// Routes (react to updates in services, route rules)
// 1. Envoy RDS response is essentially an array of virtual hosts on a port or TCP routes on a port.
// 2. Every HTTP service in the system will have a virtual host and atleast a default route (/)
// 3. The HTTP service could have additional routes based on route rules.
// 4. Every TCP service in the system will have a TCPRoute (or a filter chain match) associated
//    with its service IP if present.
// 5. If a TCP service has no service IP, it will have a listener on the service port and a
//    catch all TCPRoute
// 6. Every HTTP port of a service will share the same route configuration (list of virtual hosts)
//    until further delineation is created by route rules.
// 7. Similarly, every TCP port of a service will share the same TCPRoute.
// 7. Routes change (add/delete/update) typically when route rules change
// 9. Routes can also change when ports are added/removed from the service

// Listeners (react to updates in services, gateways, ext services)
// 1. Every unique port in the system will have an associated listener.
// 2. A service can generate one or more listeners based on the number of ports it listens on
// 3. New listeners are added when a service (incl gateway/external service) with unique
//    (previously unused) set of ports are added.
// 4. Listeners are updated when one or more services/gateways/extsvs are added/removed to/from
//    the same listener port.
// 5. Listeners are removed when there are no services/gateways/extsvcs listening on the listener
//    port.

// The indices here are doubly linked. A pair of objects are dependent on each other if they have
// to be updated when one changes.

// BoxedVirtualHost holds the virtual host proto representation and pointers to
// owning route configurations.
type BoxedVirtualHost struct {
	// The proto representation of this virtual host
	protoVirtualHost *xdsapi.VirtualHost
	// list of route configurations (RDS responses) that hold this virtual host
	routeConfigurations []*BoxedRouteConfiguration
}

// BoxedRouteConfiguration holds the route configuration proto representation and pointers to
// owning listeners
type BoxedRouteConfiguration struct {
	// The proto representation of this route configuration
	protoRouteConfiguration *xdsapi.RouteConfiguration
	// list of virtual hosts attached to this route configuration
	virtualHosts []*BoxedVirtualHost
	// list of listeners that use this route configuration
	listeners []*xdsapi.Listener
}

// BoxedEndpoint holds the endpoint proto representation and pointers to
// owning clusters
type BoxedEndpoint struct {
	// The proto representation of this endpoint
	protoEndpoint *xdsapi.LbEndpoint
	// list of clusters that contain this endpoint
	clusters []*BoxedCluster
	// list of labels associated with this endpoint
	labels []*model.Labels
}

// BoxedCluster holds the cluster proto representation, pointers to endpoints
type BoxedCluster struct {
	// The proto representation of this cluster
	protoCluster *xdsapi.Cluster
	// list of endpoints associated with this cluster
	endpoints []*BoxedEndpoint
	// List of labels associated with this cluster
	labels []*model.Labels
}

// Indices is a set of reverse indices to quickly locate endpoints, clusters, routes
// and listeners
type Indices struct {
	// Mutex guards guarantees consistency of updates to members shared across threads.
	mu sync.RWMutex

	// Map of listeners across the system indexed by port
	listeners map[int]*xdsapi.Listener

	// Map of virtual hosts indexed by name (not DNS). When a route rule has source based/gateway
	// based match, the virtual host name has to be tweaked accordingly.
	virtualHosts map[string]*BoxedVirtualHost

	// Map of route configurations (used by RDS in each listener) indexed by name.
	routeConfigs map[string]*BoxedRouteConfiguration

	// Map of clusters indexed by name
	clusters map[string]*BoxedCluster

	// Map of UID to the network endpoint. The UID is expected to be unique across the Mesh.
	endpoints map[string]*BoxedEndpoint

	// Map of a "single" label string ("key=value") to the list of clusters that use this label.
	labelToClusters map[string][]*BoxedCluster
}
