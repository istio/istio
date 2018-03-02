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

// Package v2 provides Mesh, Package v2 provides Mesh, a environment independent
// abstraction used to synchronize the list of endpoints used by Pilot
// with those watched by environment specific registries. It implements
// all the necessary logic that's used for service discovery based on
// routing rules and endpoint subsets.
// Typical Usage:
//
//     import "istio.io/istio/pilot/model"
//
//     type MyServiceRegistry struct {
//       Mesh
//     }
//     ...
//     mpr := MyServiceRegistry{model.NewMesh()}
//     ...
//     var allEndpoints []*model.ServiceInstances
//     nativeEndpoints = buildYourServiceEndpointList()
//     subsetEndpoints := make([]*Endpoint, 0, len(nativeEndpoints))
//	   for _, nativeEp := range nativeEndpoints {
//       // Create mesh endpoint from relevant values of nativeEp
//	     subsetEndpoints = append(subsetEndpoints, NewEndpoint(......))
//     }
//     err := mpr.Reconcile(subsetEndpoints)
//
// Internally xDS will use the following interfaces for:
// (Service-)Cluster Discovery Service:
//
//     serviceClusters := mesh.SubsetNames()
//
// Endpoint Discovery Service:
//
//     var listOfSubsets []string{}
//     listOfSubsets := somePkg.FigureOutSubsetsToQuery()
//     subsetEndpoints := mesh.SubsetEndpoints(listOfSubsets)
//
// Internally Galley will use the following interfaces for
// updating Pilot configuration:
//
//      var routeRuleChanges []RuleChange
//      routeRuleChanges := somePkg.FigureOutRouteRuleChanges()
//      err := mesh.UpdateRules(routeRuleChanges)
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

	"github.com/envoyproxy/go-control-plane/envoy/api/v2/core"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/endpoint"
	types "github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"

	route "istio.io/api/networking/v1alpha3"
	"istio.io/istio/pkg/log"
)

const (
	// TODO: move istioConfigFilter to github.com/istio.io/api
	// The internal filter name for attributes that should be used for
	// constructing Istio attributes on the Endpoint in reverse DNS
	// form. See core.Metadata.Name
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

// Mesh is a environment independent abstraction used by service registries for maintaining a list of service endpoints used by this Pilot.
//
// Service registries under pilot/pkg/serviceregistry
// update the list of service endpoints in this Mesh with those
// available in a environment specific service registry.
//
// Under the hoods, Mesh implements the necessary logic required for endpoint
// discovery based on routing rules and endpoint subsets.
// See https://github.com/istio/api/search?q=in%3Afile+"message+Subset"+language%3Aproto
// This logic includes comparing the updated list provided by Controllers with
// what this view holds and accordingly updating internal structures used for
// endpoint discovery.
type Mesh struct {
	// Mutex guards guarantees consistency of updates to members shared across
	// threads.
	mu sync.RWMutex

	// allEndpoints is a map of the UID to the server instance. The UID is expected
	// to be unique across Mesh. Mesh uses this map during updates from
	// Controllers to locate the previous representation  of the endpoint and check
	// for changes to its metadata that could result in changes to the subsetEndpoints.
	allEndpoints map[string]*Endpoint

	// subsetDefinitions holds the metadata of the subset i.e. the combination of
	// the destination address and the subset name supplied during configuration.
	subsetDefinitions map[string]*route.Subset

	// subsetEndpoints allows for look-ups from the Subset name to a set of Endpoints.
	// The endpoints in the set are guaranteed to exist in allEndpoints and the
	// corresponding Endpoint is guaranteed to satisfy the labels in the corresponding
	// Subset.
	subsetEndpoints subsetEndpoints

	// reverseAttrMap provides reverse lookup from label name > label value >
	// endpointSet. All Endpoints in the set are guaranteed to match the
	// attribute with the label name and value. Mesh uses the map for quickly
	// updating subsetEndpoints in response to subset label changes.
	reverseAttrMap labelEndpoints

	// reverseEpSubsets provides a reverse lookup from endpoint key to the set of
	// subsets this endpoint matched to. All subset keys associated with the endpoint
	// are guaranteed to exist in subsetDefinitions. Mesh uses this to reverse
	// lookup all subsets impacted by changes to Endpoint metadata.
	reverseEpSubsets endpointSubsets
}

// Endpoint is a environment independent representation of a Mesh Endpoints that
// uses Envoy v2 API's LbEndpoint as its internal implementation. It also
// provides utility methods intended for environment specific service registries.
type Endpoint endpoint.LbEndpoint

// endpointSet is a unique set of Endpoints.
type endpointSet map[*Endpoint]bool

// labelEndpoints is a reverse lookup map from label name and label value to
// set of *Endpoint that are guaranteed to have the label with the value.
type labelEndpoints map[string]endpointSet

// endpointSubsets maps an endpoint to a set of matching subset names.
type endpointSubsets map[*Endpoint]map[string]bool

// subsetEndpoints maps an subset name to a set of matching endpoints.
type subsetEndpoints map[string]endpointSet

// RuleChange encapsulates changes to Route Destination Rules
type RuleChange struct {
	// Rule routing/v1alpha2/destination_rule.proto
	Rule *route.DestinationRule
	// Type of destination rule config change
	Type ConfigChangeType
}

// EndpointChange is intended for incremental updates from service registries
type EndpointChange struct {
	// Endpoint the endpoint being added, deleted or updated
	Endpoint *endpoint.Endpoint
	// Type of config change
	Type ConfigChangeType
}

// EndpointLabel is intended for registry provided labels on Endpoints.
type EndpointLabel struct {
	Name  string
	Value string
}

// NewMesh creates a new empty Mesh for use by Controller implementations
func NewMesh() *Mesh {
	return &Mesh{
		allEndpoints:      map[string]*Endpoint{},
		subsetDefinitions: map[string]*route.Subset{},
		subsetEndpoints:   subsetEndpoints{},
		reverseAttrMap:    labelEndpoints{},
		reverseEpSubsets:  endpointSubsets{},
		mu:                sync.RWMutex{},
	}
}

// Reconcile is intended to be called by individual service registries to
// update Mesh with the latest list of endpoints that make up the
// view into it's associated service registry. There should be only one
// thread calling Reconcile and the endpoints passed to Reconcile must represent
// the complete set of endpoints retrieved for that environment's service registry.
// The supplied endpoints should only have been created via NewEndpoint()
func (m *Mesh) Reconcile(endpoints []*Endpoint) error {
	var errs error
	// Start out with everything that's provided by the controller and only retain
	// what's not currently in m.
	epsToAdd := make(map[string]*Endpoint, len(endpoints))
	for _, ep := range endpoints {
		epMetadata := ep.getIstioMetadata()
		epUIDValue := epMetadata[DestinationUID.AttrName()]
		epUID := epUIDValue.GetStringValue()
		if epUID == "" {
			errs = multierror.Append(errs, fmt.Errorf("mandatory attribute %q missing in endpoint metadata %+v", DestinationUID.AttrName(), ep))
			continue
		}
		epsToAdd[epUID] = ep
	}
	if errs != nil {
		errs = multierror.Prefix(errs, "errors reconciling endpoints with mesh")
		log.Error(errs.Error())
		return errs
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// Start out with everything in m and only retain what's not in supplied endpoints.
	epsToDelete := make(map[string]*Endpoint, len(m.allEndpoints))
	for uid, ep := range m.allEndpoints {
		epsToDelete[uid] = ep
	}
	for uid, expectedEp := range epsToAdd {
		existingEp, found := epsToDelete[uid]
		if !found {
			continue // expectedEp will be added
		}
		if !reflect.DeepEqual(*expectedEp, *existingEp) {
			continue // expectedEp will be added, existingEp will be deleted
		}
		// endpoint metadata has remained the same, do not add or delete
		delete(epsToAdd, uid)
		delete(epsToDelete, uid)
	}
	if len(epsToAdd) == 0 && len(epsToDelete) == 0 {
		return nil
	}
	m.deleteEndpoints(epsToDelete)
	newLabelMappings, newSubsetMappings, newEndpointSubsets := m.addEndpoints(epsToAdd)

	// Update mesh for new label mappings and new subset mappings
	m.reverseAttrMap.mergeLabelEndpoints(newLabelMappings)
	m.subsetEndpoints.mergeSubsetEndpoints(newSubsetMappings)
	m.reverseEpSubsets.mergeEndpointSubsets(newEndpointSubsets)
	return nil
}

// ReconcileDeltas allows registies to update Meshes incrementally with only those Endpoints that have changed.
// TODO: Needs implementation.
func (m *Mesh) ReconcileDeltas(endpointChanges []EndpointChange) error {
	return errors.New("unsupported interface, use Reconcile() instead")
}

// SubsetEndpoints implements functionality required for EDS and returns a list of endpoints
// that match one or more subsets.
func (m *Mesh) SubsetEndpoints(subsetNames []string) []*Endpoint {
	m.mu.RLock()
	epSet := endpointSet{}
	for _, name := range subsetNames {
		eps, found := m.subsetEndpoints[name]
		if !found {
			continue
		}
		epSet.mergeEndpoints(eps)
	}
	m.mu.RUnlock()
	out := make([]*Endpoint, 0, len(epSet))
	for ep := range epSet {
		out = append(out, ep)
	}
	return out
}

// SubsetNames implements functionality required for CDS and returns a list of all subset names currently configured for this Mesh
func (m *Mesh) SubsetNames() []string {
	out := make([]string, 0, len(m.subsetEndpoints))
	m.mu.RLock()
	defer m.mu.RUnlock()
	for name := range m.subsetEndpoints {
		out = append(out, name)
	}
	return out
}

// UpdateRules updates Mesh with changes to DestinationRules affecting Mesh.
// It updates Mesh for supplied events, adding, updating and deleting destination
// rules from this mesh depending on the corresponding ruleChange.Type.
func (m *Mesh) UpdateRules(ruleChanges []RuleChange) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs error
	for _, ruleChange := range ruleChanges {
		rule := ruleChange.Rule
		ruleType, labelValue, cidrNet := getDestinationRuleType(rule.Name)
		for _, subset := range rule.Subsets {
			key := rule.Name + RuleSubsetSeparator + subset.Name
			// A config update is interpreted as a delete followed by an add
			// and therefore appears in both the following if blocks!!
			if ruleChange.Type == ConfigDelete || ruleChange.Type == ConfigUpdate {
				oldEps := m.subsetEndpoints[key]
				m.reverseEpSubsets.deleteSubsetForEndpoints(oldEps, key)
				delete(m.subsetEndpoints, key)
				delete(m.subsetDefinitions, key)
			}
			if ruleChange.Type == ConfigAdd || ruleChange.Type == ConfigUpdate {
				var labelsToMatch map[string]string
				switch ruleType {
				case DestinationRuleService:
					labelsToMatch =
						newMapWithLabelValue(subset.GetLabels(), DestinationService.AttrName(), labelValue)
				case DestinationRuleIP:
					labelsToMatch =
						newMapWithLabelValue(subset.GetLabels(), DestinationIP.AttrName(), labelValue)
				case DestinationRuleName:
					labelsToMatch =
						newMapWithLabelValue(subset.GetLabels(), DestinationName.AttrName(), labelValue)
				case DestinationRuleCIDR, DestinationRuleWildcard:
					// Direct matches will not work. Scoping is done later.
					labelsToMatch = subset.GetLabels()
				}
				newEps := m.reverseAttrMap.getEndpointsMatching(labelsToMatch)
				if ruleType == DestinationRuleCIDR || ruleType == DestinationRuleWildcard {
					// For rule names that do not map to values that can be label matched
					// scope the returned set to the type of rule.
					newEps = newEps.scopeToRule(ruleType, labelValue, cidrNet)
				}
				m.subsetEndpoints.addEndpointSet(key, newEps)
				m.reverseEpSubsets.addSubsetForEndpoints(newEps, key)
				m.subsetDefinitions[key] = subset
			}
		}
	}
	return errs
}

// addEndpoints adds Endpoints in epsToAdd to Mesh and returns a reverseAttrMap and the forward/reverse subset to endpoint mappings for subsequent merging.
//
// The caller is expected to lock m.mu before calling this method().
func (m *Mesh) addEndpoints(
	epsToAdd map[string]*Endpoint) (newLabelMappings labelEndpoints, newSubsetMappings subsetEndpoints, newEndpointSubsets endpointSubsets) {
	if len(epsToAdd) == 0 {
		return labelEndpoints{}, subsetEndpoints{}, endpointSubsets{}
	}
	newLabelMappings = labelEndpoints{}
	newSubsetMappings, newEndpointSubsets = newLabelMappings.addEndpoints(epsToAdd)

	for uid, addEp := range epsToAdd {
		m.allEndpoints[uid] = addEp
	}
	// Verify if existing subset definitions apply for the newly added labels
	for name, subset := range m.subsetDefinitions {
		matchingEpKeys := newLabelMappings.getEndpointsMatching(subset.Labels)
		newSubsetMappings.addEndpointSet(name, matchingEpKeys)
	}
	return newLabelMappings, newSubsetMappings, newEndpointSubsets
}

// deleteEndpoints removes all internal references of the endpoints in the map from this mesh.
// The endpoint reference is removed from allEndpoints, subsetEndpoints, reverseAttrMap
// and reverseEpSubsets. This method is expected to be called only from inside Reconcile().
//
// The caller is expected to lock m.mu before calling this method().
func (m *Mesh) deleteEndpoints(endpoints map[string]*Endpoint) {
	for uid, ep := range endpoints {
		// Remove references from reverseAttrMap
		for label, value := range ep.getSingleValuedAttrs() {
			epSet := m.reverseAttrMap[label+nameValueSeparator+value]
			delete(epSet, ep)
		}
		for _, value := range ep.getMultiValuedAttrs(DestinationService.AttrName()) {
			m.reverseAttrMap.deleteLabelMapping(DestinationService.AttrName(), value, ep)
		}
		for _, value := range ep.getMultiValuedAttrs(DestinationDomain.AttrName()) {
			m.reverseAttrMap.deleteLabelMapping(DestinationService.AttrName(), value, ep)
		}

		// Remove references from reverseEpSubsets and subsetEndpoints
		subsets, subsetsFound := m.reverseEpSubsets[ep]
		if subsetsFound {
			for name := range subsets {
				subsetEps, epsFound := m.subsetEndpoints[name]
				if epsFound {
					delete(subsetEps, ep)
				}
				// Delete on empty only if this is a service only subset.
				// Empty rule scoped subsets are perfectly valid.
				if !strings.Contains(name, RuleSubsetSeparator) && len(subsetEps) == 0 {
					delete(m.subsetEndpoints, name)
				}
			}
			delete(m.reverseEpSubsets, ep)
		}
		// Remove entry from allEndpoints
		delete(m.allEndpoints, uid)
	}
}

// addEndpoints maps Endpoints in epsToAdd to attributes of the ep and returns the forward and reverse DestinationService to endpoint maps.
func (le labelEndpoints) addEndpoints(epsToAdd map[string]*Endpoint) (newSubsetMappings subsetEndpoints, newEndpointSubsets endpointSubsets) {
	newSubsetMappings = subsetEndpoints{}
	newEndpointSubsets = endpointSubsets{}
	for _, ep := range epsToAdd {
		le.addLabelMap(ep.getSingleValuedAttrs(), ep)
		le.addLabelValues(DestinationDomain.AttrName(), ep.getMultiValuedAttrs(DestinationDomain.AttrName()), ep)
		destServices := ep.getMultiValuedAttrs(DestinationService.AttrName())
		// By default we treat service FQDNs as subset names, only these subsets would not have any subset labels,
		// i.e. querying by FQDNs will retrieve all endpoints for that service.
		le.addLabelValues(DestinationService.AttrName(), destServices, ep)
		newSubsetMappings.addSubsetsToEndpoint(destServices, ep)
		subsets := map[string]bool{}
		for _, destService := range destServices {
			subsets[destService] = true
		}
		newEndpointSubsets[ep] = subsets
	}
	return newSubsetMappings, newEndpointSubsets
}

// getEndpointsMatching returns a set of endpoints that match the values of all labels with a reasonably predictable performance.
// It does this by fetching the Endpoint sets for for each matching label value combination then uses the shortest set for checking whether those
// endpoints are present in the other larger endpoint sets.
func (le labelEndpoints) getEndpointsMatching(labels map[string]string) endpointSet {
	countLabels := len(labels)
	if countLabels == 0 {
		// There must be at least one label else return nothing
		return endpointSet{}
	}
	// Note: 0th index has the smallest endpoint set
	matchingSets := make([]endpointSet, 0, countLabels)
	smallestSetLen := math.MaxInt32
	for l, v := range labels {
		epSet, found := le[l+nameValueSeparator+v]
		if !found {
			// Nothing matched at least one label
			return endpointSet{}
		}
		matchingSets = append(matchingSets, epSet)
		lenKeySet := len(epSet)
		if lenKeySet < smallestSetLen {
			smallestSetLen = lenKeySet
			justAdded := len(matchingSets) - 1
			if justAdded > 0 {
				swappedMatchingSet := matchingSets[0]
				matchingSets[0] = matchingSets[justAdded]
				matchingSets[justAdded] = swappedMatchingSet
			}
		}
	}
	if countLabels == 1 {
		return matchingSets[0]
	}
	out := newEndpointSet(matchingSets[0])
	for uid := range out {
		for setIdx := 1; setIdx < countLabels; setIdx++ {
			_, found := matchingSets[setIdx][uid]
			if !found {
				delete(out, uid)
				break
			}
		}
	}
	return out
}

// addLabelValues adds mappings to ep for each of the multi-valued attribute with name labelName.
func (le labelEndpoints) addLabelValues(labelName string, values []string, ep *Endpoint) {
	for _, value := range values {
		labelKey := labelName + nameValueSeparator + value
		epSet, found := le[labelKey]
		if !found {
			epSet = make(endpointSet)
			le[labelKey] = epSet
		}
		epSet[ep] = true
	}
}

// addLabelMap adds mappings to ep for each of the label values in labelMap.
func (le labelEndpoints) addLabelMap(labelMap map[string]string, ep *Endpoint) {
	for labelName, value := range labelMap {
		labelKey := labelName + nameValueSeparator + value
		epSet, found := le[labelKey]
		if !found {
			epSet = make(endpointSet)
			le[labelKey] = epSet
		}
		epSet[ep] = true
	}
}

// deleteLabelMapping removes the endpoint from the set of endpoints associated with the labelKey which is built from the label and value.
func (le labelEndpoints) deleteLabelMapping(labelName, labelValue string, ep *Endpoint) {
	labelKey := labelName + nameValueSeparator + labelValue
	epSet := le[labelKey]
	delete(epSet, ep)
	if len(epSet) == 0 {
		delete(le, labelKey)
	}
}

// mergeLabelEndpoints merges other with le.
func (le labelEndpoints) mergeLabelEndpoints(other labelEndpoints) {
	for labelKey, otherEps := range other {
		if len(otherEps) == 0 {
			continue
		}
		epSet, found := le[labelKey]
		if !found {
			epSet = make(endpointSet, len(otherEps))
			le[labelKey] = epSet
		}
		for ep := range otherEps {
			epSet[ep] = true
		}
	}
}

// newEndpointSet creates a copy of eps that can be modified without altering eps.
func newEndpointSet(eps endpointSet) endpointSet {
	out := make(endpointSet, len(eps))
	for ep, v := range eps {
		if v {
			out[ep] = v
		}
	}
	return out
}

// mergeEndpoints merges endpoints from other into this eps.
func (eps endpointSet) mergeEndpoints(other endpointSet) {
	for ep, v := range other {
		if v {
			eps[ep] = v
		}
	}
}

// scopeToRule returns a subset of this eps by matching either the wildcard domain or CIDR depending on ruleType.
// This is an expensive method, but is only used for determining new subsets when Rules are updated.
// For wildcard domains, if the domainSuffix is empty, this will match all endpoints in the set that have the domain
// attribute set.
func (eps endpointSet) scopeToRule(ruleType DestinationRuleType, domainSuffix string, cidrNet *net.IPNet) endpointSet {
	out := make(endpointSet, len(eps))
	switch ruleType {
	case DestinationRuleWildcard:
		for ep := range eps {
			if ep.matchDomainSuffix(domainSuffix) {
				out[ep] = true
			}
		}
	case DestinationRuleCIDR:
		for ep := range eps {
			epIP := net.ParseIP(ep.Endpoint.Address.GetSocketAddress().Address)
			if cidrNet.Contains(epIP) {
				out[ep] = true
			}
		}
	}
	return out
}

// addSubsetForEndpoints updates es by adding subsetName to the set of subset names associated with each of the endpoints in epSet.
func (es endpointSubsets) addSubsetForEndpoints(eps endpointSet, subsetName string) {
	for ep := range eps {
		names, found := es[ep]
		if !found {
			names = map[string]bool{}
			es[ep] = names
		}
		names[subsetName] = true
	}
}

// mergeEndpointSubsets merges contents of other with this map.
func (es endpointSubsets) mergeEndpointSubsets(other endpointSubsets) {
	for ep, subsets := range other {
		names, found := es[ep]
		if !found {
			es[ep] = subsets
			continue
		}
		for name := range subsets {
			names[name] = true
		}
	}
}

// deleteSubsetForEndpoints updates es by removing subsetName from the endpoint's set of subset names for each of the endpoints in epSet.
func (es endpointSubsets) deleteSubsetForEndpoints(eps endpointSet, subsetName string) {
	for ep := range eps {
		names, found := es[ep]
		if !found {
			return
		}
		delete(names, subsetName)
		if len(names) == 0 {
			delete(es, ep)
		}
	}
}

// addSubsetsToEndpoint updates se by merging subsetNames mapped to ep.
func (se subsetEndpoints) addSubsetsToEndpoint(subsetNames []string, ep *Endpoint) {
	for _, subsetName := range subsetNames {
		epUIDSet, found := se[subsetName]
		if !found {
			epUIDSet = endpointSet{}
			se[subsetName] = epUIDSet
		}
		epUIDSet[ep] = true
	}
}

// addEndpointSet udates se by adding a set of endpoints mapped to subsetName.
func (se subsetEndpoints) addEndpointSet(subsetName string, otherEps endpointSet) {
	if len(otherEps) == 0 {
		return
	}
	epSet, found := se[subsetName]
	if !found {
		epSet = make(endpointSet, len(otherEps))
		se[subsetName] = epSet
	}
	for endpoint := range otherEps {
		epSet[endpoint] = true
	}
}

// mergeSubsetEndpoints merges others with se.
func (se subsetEndpoints) mergeSubsetEndpoints(other subsetEndpoints) {
	for subsetName, epKetSet := range other {
		se.addEndpointSet(subsetName, epKetSet)
	}
}

// NewEndpoint is a boiler-plate function intended for environment specific registries to create a new Endpoint.
// This method ensures all the necessary data required for creating subsets are correctly setup. It
// also performs sorting of arrays etc, to allow stable results for reflect.DeepEquals() for quick
// comparisons. The network address	of the endpoint must be accessible from Pilot. If the registry creating the
// endpoint is for a remote Pilot, the endpoint's address may be that of an Istio gateway which must be accessible
// from Pilot. The gateway, itself, may have more than one Endpoints behind it that are not directly network accessible
// from Pilot. Similarly the the network port of this endpoint that must be accessible from Pilot.
// socketProtocol should be set to TCP or UPD. Labels are properties of the workload, for example: pod labels in Kubernetes.
func NewEndpoint(address string, port uint32, socketProtocol SocketProtocol, labels []EndpointLabel) (*Endpoint, error) {
	var errs error
	ipAddr := net.ParseIP(address)
	if ipAddr == nil {
		errs = multierror.Append(errs, fmt.Errorf("invalid IP address %q", address))
	}
	hasUID := false
	svcFQDNs := []string{}
	svcDomains := []string{}
	miscLabels := make(map[string]string, len(labels))
	for _, label := range labels {
		if prohibitedAttrSet[label.Name] {
			log.Warnf("ignoring prohibited use of Istio destination label %q for endpoint label", label.Name)
			continue
		}
		switch label.Name {
		case DestinationService.AttrName():
			svcFQDNs = append(svcFQDNs, label.Value)
		case DestinationDomain.AttrName():
			svcDomains = append(svcDomains, label.Value)
		case DestinationUID.AttrName():
			hasUID = true
			fallthrough
		default:
			oldValue, found := miscLabels[label.Name]
			if found {
				log.Warnf("ignoring multiple values [%q,%q] for single valued label %q", oldValue, label.Value, label.Name)
				continue
			}
			miscLabels[label.Name] = label.Value
		}
	}
	if !hasUID {
		errs = multierror.Append(errs, fmt.Errorf("missing Endpoint mandatory label %q", DestinationUID.AttrName()))
	}
	if len(svcFQDNs) == 0 {
		errs = multierror.Append(errs, fmt.Errorf("missing Endpoint mandatory label %q", DestinationService.AttrName()))
	}
	if errs != nil {
		return nil, multierror.Prefix(errs, "Mesh endpoint creation errors")
	}
	var epSocketProtocol core.SocketAddress_Protocol
	switch socketProtocol {
	case SocketProtocolTCP:
		epSocketProtocol = core.TCP
	case SocketProtocolUDP:
		epSocketProtocol = core.UDP
	}
	miscLabels[DestinationIP.AttrName()] = ipAddr.String()
	miscLabels[DestinationPort.AttrName()] = strconv.Itoa((int)(port))
	ep := Endpoint{
		Endpoint: &endpoint.Endpoint{
			Address: &core.Address{
				Address: &core.Address_SocketAddress{
					&core.SocketAddress{
						Address:    address,
						Ipv4Compat: ipAddr.To4() != nil,
						Protocol:   epSocketProtocol,
						PortSpecifier: &core.SocketAddress_PortValue{
							PortValue: port,
						},
					},
				},
			},
		},
	}

	// Populate endpoint labels.
	ep.setSingleValuedAttrs(miscLabels)
	// Sort for stable comparisons down the line.
	sort.Strings(svcFQDNs)
	ep.setMultiValuedAttrs(DestinationService.AttrName(), svcFQDNs)
	sort.Strings(svcDomains)
	ep.setMultiValuedAttrs(DestinationDomain.AttrName(), svcDomains)
	return &ep, nil
}

// getSingleValuedAttrs returns a map of single valued labels.
func (ep *Endpoint) getSingleValuedAttrs() map[string]string {
	metadataFields := ep.getIstioMetadata()
	if metadataFields == nil {
		return nil
	}
	out := make(map[string]string, len(metadataFields))
	for attrName, attrValue := range metadataFields {
		labelValue := attrValue.GetStringValue()
		if labelValue == "" {
			continue
		}
		out[attrName] = labelValue
	}
	return out
}

// getMultiValuedAttrs returns a list of values for a multi-valued label attrName.
func (ep *Endpoint) getMultiValuedAttrs(attrName string) []string {
	metadataFields := ep.getIstioMetadata()
	if metadataFields == nil {
		return nil
	}
	attrValue, found := metadataFields[attrName]
	if !found {
		return nil
	}
	valueList := attrValue.GetListValue()
	if valueList == nil {
		return nil
	}
	out := make([]string, 0, len(valueList.Values))
	for _, value := range valueList.Values {
		out = append(out, value.GetStringValue())
	}
	return out
}

// setSingleValuedAttrs sets up the endpoint with the supplied single-valued labels.
func (ep *Endpoint) setSingleValuedAttrs(labels map[string]string) {
	istioMeta := ep.createIstioMetadata()
	for k, v := range labels {
		istioMeta[k] = &types.Value{
			&types.Value_StringValue{v},
		}
	}
}

// setMultiValuedAttrs sets the multi-values attribute attrName with attrValues.
// Currently used internally for service name and svcAliases only.
func (ep *Endpoint) setMultiValuedAttrs(attrName string, attrValues []string) {
	istioMeta := ep.createIstioMetadata()
	listValues := make([]*types.Value, 0, len(attrValues))
	for _, attrValue := range attrValues {
		listValues = append(listValues, &types.Value{&types.Value_StringValue{attrValue}})
	}
	istioMeta[attrName] = &types.Value{&types.Value_ListValue{&types.ListValue{listValues}}}
}

// createIstioMetadata returns the internal label store for the endpoint, creating one if necessary.
func (ep *Endpoint) createIstioMetadata() map[string]*types.Value {
	metadata := ep.Metadata
	if metadata == nil {
		metadata = &core.Metadata{}
		ep.Metadata = metadata
	}
	filterMap := metadata.FilterMetadata
	if filterMap == nil {
		filterMap = map[string]*types.Struct{}
		metadata.FilterMetadata = filterMap
	}
	configLabels := filterMap[istioConfigFilter]
	if configLabels == nil {
		configLabels = &types.Struct{}
		filterMap[istioConfigFilter] = configLabels
	}
	if configLabels.Fields == nil {
		configLabels.Fields = map[string]*types.Value{}
	}
	return configLabels.Fields
}

// getIstioMetadata returns the internal implementation of the label store for the endpoint, returning nil the internal implementation doesn't have one.
func (ep *Endpoint) getIstioMetadata() map[string]*types.Value {
	metadata := ep.Metadata
	if metadata == nil {
		return nil
	}
	filterMap := metadata.FilterMetadata
	if filterMap == nil {
		return nil
	}
	configLabels := filterMap[istioConfigFilter]
	if configLabels == nil {
		return nil
	}
	return configLabels.GetFields()
}

// matchDomainSuffix returns true if any of the domains attribute for this Endpoint match the domain suffix.
// If the domainSuffix is empty, it matches any endpoints that have a domain attribute set.
func (ep *Endpoint) matchDomainSuffix(domainSuffix string) bool {
	for _, epDomainName := range ep.getMultiValuedAttrs(DestinationDomain.AttrName()) {
		if domainSuffix == "" {
			return true
		}
		if strings.HasSuffix(epDomainName, domainSuffix) {
			return true
		}
	}
	return false
}

// getDestinationRuleType parses ruleName and returns an appropriate DestinationRuleType, the associated dns domain suffix or the CIDR for matching endpoints.
// It returns the value to use for querying subsets, ex: for a  ruleName with a DNS wildcard,
// it returns the suffix to use for the domain. For an IP address, it returns a normalized address.
// For CIDRs, the query value is set to nil, but a IP network corresponding to the CIDR specified in
// ruleName is returned. For all other types, the returned IP Network is nil.
func getDestinationRuleType(ruleName string) (DestinationRuleType, string, *net.IPNet) {
	if wildcardDomainRegex.MatchString(ruleName) {
		switch {
		case ruleName == "*":
			return DestinationRuleWildcard, "", nil
		case strings.HasPrefix(ruleName, "*-"):
			return DestinationRuleWildcard, ruleName[2:], nil
		case strings.HasPrefix(ruleName, "*"):
			return DestinationRuleWildcard, ruleName[1:], nil
		}
	}
	_, ciderNet, cidrErr := net.ParseCIDR(ruleName)
	if cidrErr == nil {
		return DestinationRuleCIDR, "", ciderNet
	}
	ip := net.ParseIP(ruleName)
	if ip != nil {
		return DestinationRuleIP, ip.String(), nil
	}
	if !strings.Contains(ruleName, ".") {
		return DestinationRuleName, ruleName, nil
	}
	return DestinationRuleService, ruleName, nil
}

// AttrName returns the string value of attr.
func (attr DestinationAttribute) AttrName() string {
	return (string)(attr)
}

// newMapWithLabelValue returns a new copy of the map with the name value mapping added to it.
func newMapWithLabelValue(labels map[string]string, name, value string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	out[name] = value
	for n, v := range labels {
		out[n] = v
	}
	return out
}
