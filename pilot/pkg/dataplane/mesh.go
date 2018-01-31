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

// Package model provides Mesh which is a platform independent
// abstraction used by Controllers to synchronize the list of endpoints
// used by the pilot with those available in the platform specific
// registry. It implements all the necessary logic that's used for service
// discovery based on routing rules and endpoint subsets.
// Typical Usage:
//
//   import "istio.io/istio/pilot/model"
//
//   type MyPlatformController struct {
//     Mesh
//   }
//   ...
//   pc := MyPlatformController{model.NewMesh()}
//   ...
//   var allEndpoints []*model.ServiceInstances
//   nativeEndpoints = buildYourPlatformServiceEndpointList()
//   SusbsetEndpoints := make([]*Endpoint, len(nativeEndpoints))
//	 for idx, nativeEp := range nativeEndpoints {
//     // Create mesh endpoint from relevant values of nativeEp
//	   SusbsetEndpoints[idx] = NewEndpoint(......)
//   }
//   pc.Reconcile(SusbsetEndpoints)
//
package dataplane

import (
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	types "github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"
	route "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pkg/log"
)

const (
	// The internal filter name for attributes that should be used for
	// constructing Istio attributes on the Endpoint in reverse DNS
	// form. See xdsapi.Metadata.Name
	istioConfigFilter = "io.istio.istio-config"

	// Key Istio attribute names for mapping endpoints to subsets.
	// For details, please see
	// https://istio.io/docs/reference/config/mixer/attribute-vocabulary.html

	// DestinationUID is the attribute name for the mesh unique, platform-specific
	// unique identifier for the server instance of the destination service. Mesh
	// uses the label to uqniquely identify the endpoint when it receives updates
	// from the Controller. Example: kubernetes://my-svc-234443-5sffe.my-namespace
	// No two instances running anywhere in the mesh can have the same value for
	// UID.
	DestinationUID DestinationAttribute = "destination.uid"

	// DestinationService represents the fully qualified name of the service that the server
	// belongs to. Example: "my-svc.my-namespace.svc.cluster.local"
	DestinationService DestinationAttribute = "destination.service"

	// DestinationName represents the short name of the service that the server
	// belongs to. Example: "my-svc"
	DestinationName DestinationAttribute = "destination.name"

	// DestinationDomain represents the domain portion of the service name, excluding
	// the name and namespace, example: svc.cluster.local
	DestinationDomain DestinationAttribute = "destination.domain"

	// DestinationIP represents the IP address of the server instance, example 10.0.0.104.
	// This IP is expected to be reachable from __THIS__ pilot. No distinction
	// is being made for directly reachable service instances versus those
	// behind a VIP. Istio's health discovery service will ensure that this
	// endpoint's capacity is correctly reported accordingly.
	DestinationIP DestinationAttribute = "destination.ip"

	// DestinationPort represents the recipient port on the server IP address, Example: 443
	DestinationPort DestinationAttribute = "destination.port"

	// nameValueSeparator is a separator for creating label name-value keys used for
	// reverse lookups on labels.
	nameValueSeparator = "\x1F"

	// RuleSubsetSeparator separates the destination rule name and the subset name for
	// the key to the subsetEndpoints map.
	RuleSubsetSeparator = "|"

	// wildCardDomainPrefix is used to check if rule names are wild card domains. If
	// the rule name is prefixed with this pattern, consider it a wild card.
	wildCardDomainPrefix = "*."
)

// Enumerated constants for ConfigChangeType.
const (
	_                          = iota
	ConfigAdd ConfigChangeType = iota
	ConfigUpdate
	ConfigDelete
)

// Enumerated constants for SocketProtocol.
const (
	_                                = iota
	SocketProtocolTCP SocketProtocol = iota
	SocketProtocolUDP
)

// Enumerated constants for DestinationRuleType.
const (
	_ = iota
	// DestinationRuleDomain is a type of destination rule where the
	// rule name is an FQDN of the service and resulting Subsets ought
	// to be further scoped to this FQDN.
	DestinationRuleDomain DestinationRuleType = iota

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
	prohibitedAttrSet map[string]bool
)

// DestinationAttribute encapsulates enums for key Istio attribute names used in Subsets
// and Endpoints
type DestinationAttribute string

// Mesh is a platform independent abstraction used by Controllers
// for maintaining a list of service endpoints used by this Pilot.
//
// Controllers under https://github.com/istio/istio/tree/master/pilot/platform
// update the list of service endpoints in this Mesh with those
// available in a platform specific service registry.
//
// Under the hoods, the Mesh implements the necessary logic required for endpoint
// discovery based on routing rules and endpoint subsets.
// See https://github.com/istio/api/search?q=in%3Afile+"message+Subset"+language%3Aproto
// This logic includes comparing the updated list provided by Controllers with
// what this view holds and accordingly updating internal structures used for
// endpoint discovery.
type Mesh struct {

	// allEndpoints is a map of the UID to the server instance. The UID is expected
	// to be unique across the mesh. Mesh uses this map during updates from
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

	// Mutex guards guarantees consistency of updates to members shared across
	// threads.
	mu sync.RWMutex
}

// Endpoint is a platform independent representation of a Mesh Endpoints that
// uses Envoy v2 API's LbEndpoint as its internal implementation. It also
// provides utility methods intended for platform specific Controllers.
type Endpoint xdsapi.LbEndpoint

// endpointSet is a unique set of Endpoints.
type endpointSet map[*Endpoint]bool

// labelEndpoints is a reverse lookup map from label name and label value to
// set of *Endpoint that are guaranteed to have the label with the value.
type labelEndpoints map[string]endpointSet

// labelValueEndpoints is a reverse lookup map from label value to
// the matching endpoint set.
type labelValueEndpoints map[string]endpointSet

// endpointSubsets maps an endpoint to a set of matching subset names.
type endpointSubsets map[*Endpoint]map[string]bool

// subsetEndpoints maps an subset name to a set of matching endpoint UIDs.
type subsetEndpoints map[string]endpointSet

// RuleChange encapsulates changes to Route Destination Rules
type RuleChange struct {
	// Rule routing/v1alpha2/destination_rule.proto
	Rule *route.DestinationRule
	// Type of destination rule config change
	Type ConfigChangeType
}

type EndpointChange struct {
	Endpoint *xdsapi.Endpoint
	Type     ConfigChangeType
}

type ConfigChangeType int

type SocketProtocol int

type DestinationRuleType int

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

// Reconcile is intended to be called by individual platform Controllers to
// update the Mesh with the latest list of endpoints that make up the
// view into it's associated service registry. There should be only one Controller
// thread calling Reconcile and the endpoints passed to Reconcile must represent
// the complete set of endpoints retrieved for that platform's service registry.
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
			errs = multierror.Append(errs, fmt.Errorf(
				"mandatory attribute '%s' missing in endpoint metadata '%v'",
				DestinationUID.AttrName(), ep))
			continue
		}
		epsToAdd[epUID] = ep
	}
	if errs != nil {
		return logAndReturnReconcileErrors(errs)
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
	for uid, delEp := range epsToDelete {
		m.deleteEndpoint(uid, delEp)
	}
	newLabelMappings := labelEndpoints{}
	newSubsetMappings := subsetEndpoints{}
	for uid, addEp := range epsToAdd {
		serviceNames := newLabelMappings.addEndpoint(addEp)
		m.allEndpoints[uid] = addEp
		// By default we create a subset with the same name as the service.
		// This provides a set of subsets that do not have any bearings on endpoint
		// labels and is used for CDS.
		for _, svcName := range serviceNames {
			newSubsetMappings.addEndpoint(svcName, addEp)
		}
	}
	// Verify if existing subset definitions apply for the newly added labels
	for ssName, subset := range m.subsetDefinitions {
		matchingEpKeys := newLabelMappings.getEndpointsMatching(subset.Labels)
		newSubsetMappings.addEndpointSet(ssName, matchingEpKeys)
	}
	// Update mesh for new label mappings and new subset mappings
	m.reverseAttrMap.addLabelEndpoints(newLabelMappings)
	m.subsetEndpoints.addSubsetEndpoints(newSubsetMappings)
	return nil
}

func (m *Mesh) ReconcileDeltas(endpointChanges []EndpointChange) error {
	return errors.New("unsupported interface, use Reconcile() instead")
}

// SusbsetEndpoints implements functionality required for EDS and returns a list of endpoints
// that match one or more subsets.
func (m *Mesh) SusbsetEndpoints(subsetNames []string) []*Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	epSet := endpointSet{}
	for _, ssName := range subsetNames {
		ssEpSet, found := m.subsetEndpoints[ssName]
		if !found {
			continue
		}
		epSet.appendAll(ssEpSet)
	}
	out := make([]*Endpoint, len(epSet))
	idx := 0
	for ep := range epSet {
		out[idx] = ep
		idx++
	}
	return out
}

// SubsetNames implements functionality required for CDS and returns a list of all subset
// names currently configured for this Mesh
func (m *Mesh) SubsetNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]string, len(m.subsetEndpoints))
	idx := 0
	for ssName := range m.subsetEndpoints {
		out[idx] = ssName
		idx++
	}
	return out
}

// UpdateRules implements functionality required for pilot configuration (via Galley).
// It updates the Mesh for supplied events, adding, updating and deleting destination
// rules from this mesh as determined by the corresponding Event.
// TODO: investigate if UpdateRules should be an all or nothing update and if required,
// separate validation steps from actual changes to Mesh.
func (m *Mesh) UpdateRules(ruleChanges []RuleChange) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var errs error
	for _, ruleChange := range ruleChanges {
		rule := ruleChange.Rule
		ruleType, labelValue, cidrNet := determineRuleScope(rule.Name)
		for _, subset := range rule.Subsets {
			scopedSSName := rule.Name + RuleSubsetSeparator + subset.Name
			if ruleChange.Type == ConfigDelete || ruleChange.Type == ConfigUpdate {
				oldEpSet := m.subsetEndpoints[scopedSSName]
				for ep := range oldEpSet {
					m.reverseEpSubsets.deleteSubset(ep, scopedSSName)
				}
				delete(m.subsetEndpoints, scopedSSName)
			}
			if ruleChange.Type == ConfigAdd || ruleChange.Type == ConfigUpdate {
				var labelsToMatch map[string]string
				switch ruleType {
				case DestinationRuleDomain:
					labelsToMatch =
						newMapWithLabelValue(subset.GetLabels(), DestinationDomain.AttrName(), labelValue)
				case DestinationRuleIP:
					labelsToMatch =
						newMapWithLabelValue(subset.GetLabels(), DestinationIP.AttrName(), labelValue)
				case DestinationRuleName:
					labelsToMatch =
						newMapWithLabelValue(subset.GetLabels(), DestinationName.AttrName(), labelValue)
				case DestinationRuleWildcard | DestinationRuleWildcard:
					// Direct matches will not work. Scoping is done later.
					labelsToMatch = subset.GetLabels()
				}
				newEpSubset := m.reverseAttrMap.getEndpointsMatching(labelsToMatch)
				if ruleType == DestinationRuleCIDR || ruleType == DestinationRuleWildcard {
					// For rule names that do not map to values that can be label matched
					// scope the returned set to the type of rule.
					newEpSubset = newEpSubset.scopeToRule(ruleType, labelValue, cidrNet)
				}
				m.subsetEndpoints.addEndpointSet(scopedSSName, newEpSubset)
			}
		}
	}
	return errs
}

// NewEndpoint is a boiler plate function intended for platform Controllers to create a new Endpoint.
// This method ensures all the necessary data required for creating subsets are correctly setup. It
// also performs sorting of arrays etc, to allow stable results for reflect.DeepEquals() for quick
// comparisons.
func NewEndpoint(address string, port uint32, socketProtocol SocketProtocol, labels []EndpointLabel) (*Endpoint, error) {
	var errs error
	ipAddr := net.ParseIP(address)
	if ipAddr == nil {
		errs = multierror.Append(errs, fmt.Errorf("invalid IP address '%s'", address))
	}
	hasUID := false
	svcNames := []string{}
	svcDomains := []string{}
	miscLabels := make(map[string]string, len(labels))
	for _, label := range labels {
		if prohibitedAttrSet[label.Name] {
			errs = multierror.Append(fmt.Errorf(
				"prohibited use of Istio destination label '%s' for endpoint label", label.Name))
			continue
		}
		switch label.Name {
		case DestinationService.AttrName():
			svcNames = append(svcNames, label.Value)
		case DestinationDomain.AttrName():
			svcDomains = append(svcDomains, label.Value)
		case DestinationUID.AttrName():
			hasUID = true
			fallthrough
		default:
			miscLabels[label.Name] = label.Value
		}
	}
	var sockAddrProtocol xdsapi.SocketAddress_Protocol
	switch socketProtocol {
	case SocketProtocolTCP:
		sockAddrProtocol = xdsapi.SocketAddress_TCP
	case SocketProtocolUDP:
		sockAddrProtocol = xdsapi.SocketAddress_UDP
	default:
		errs = multierror.Append(errors.New(
			"invalid value for Endpoint socket protocl must be one of SocketProtocolUDP or SocketProtocolTCP."))
	}
	if !hasUID {
		errs = multierror.Append(fmt.Errorf(
			"missing Endpoint mandatory label '%s'", DestinationUID.AttrName()))
	}
	if len(svcNames) == 0 {
		errs = multierror.Append(fmt.Errorf(
			"missing Endpoint mandatory label '%s'", DestinationService.AttrName()))
	}
	if errs != nil {
		return nil, multierror.Prefix(errs, "Mesh endpoint creation errors")
	}
	miscLabels[DestinationIP.AttrName()] = ipAddr.String()
	miscLabels[DestinationPort.AttrName()] = strconv.Itoa((int)(port))
	ep := Endpoint{
		Endpoint: &xdsapi.Endpoint{
			Address: &xdsapi.Address{
				Address: &xdsapi.Address_SocketAddress{
					&xdsapi.SocketAddress{
						Address:    address,
						Ipv4Compat: ipAddr.To4() != nil,
						Protocol:   sockAddrProtocol,
						PortSpecifier: &xdsapi.SocketAddress_PortValue{
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
	sort.Strings(svcNames)
	ep.setMultiValuedAttrs(DestinationService.AttrName(), svcNames)
	sort.Strings(svcDomains)
	ep.setMultiValuedAttrs(DestinationDomain.AttrName(), svcDomains)
	return &ep, nil
}

func determineRuleScope(ruleName string) (DestinationRuleType, string, *net.IPNet) {
	if strings.HasPrefix(ruleName, wildCardDomainPrefix) {
		return DestinationRuleDomain, ruleName[2:], nil
	}
	_, ciderNet, cidrErr := net.ParseCIDR(ruleName)
	if cidrErr != nil {
		return DestinationRuleCIDR, "", ciderNet
	}
	ip := net.ParseIP(ruleName)
	if ip != nil {
		return DestinationRuleIP, ip.String(), nil
	}
	if !strings.Contains(ruleName, ".") {
		return DestinationRuleName, ruleName, nil
	}
	return DestinationRuleDomain, ruleName, nil
}

func (ep *Endpoint) getSingleValuedAttrs() map[string]string {
	metadataFields := ep.getIstioMetadata()
	if metadataFields == nil {
		return nil
	}
	var out map[string]string
	for attrName, attrValue := range metadataFields {
		labelValue := attrValue.GetStringValue()
		if labelValue == "" {
			continue
		}
		out[attrName] = labelValue
	}
	return out
}

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
	out := make([]string, len(valueList.Values))
	for idx, value := range valueList.Values {
		out[idx] = value.GetStringValue()
	}
	return out
}

// setSingleValuedAttrs is ....?????
func (ep *Endpoint) setSingleValuedAttrs(labels map[string]string) {
	istioMeta := ep.createIstioMetadata()
	for k, v := range labels {
		istioMeta[k] = &types.Value{
			&types.Value_StringValue{v},
		}
	}
}

// setMultiValuedAttrs allows multiple values to be set for a given attribute.
// Currently used internally for service name and svcAliases only.
func (ep *Endpoint) setMultiValuedAttrs(attrName string, attrValues []string) {
	istioMeta := ep.createIstioMetadata()
	listValues := make([]*types.Value, len(attrValues))
	for idx, attrValue := range attrValues {
		listValues[idx] = &types.Value{&types.Value_StringValue{attrValue}}
	}
	istioMeta[attrName] = &types.Value{&types.Value_ListValue{&types.ListValue{listValues}}}
}

func (ep *Endpoint) createIstioMetadata() map[string]*types.Value {
	metadata := ep.Metadata
	if metadata == nil {
		metadata = &xdsapi.Metadata{}
		ep.Metadata = metadata
	}
	filterMap := metadata.GetFilterMetadata()
	if filterMap == nil {
		metadata.FilterMetadata = map[string]*types.Struct{}
	}
	configLabels := filterMap[istioConfigFilter]
	if configLabels == nil {
		configLabels = &types.Struct{}
		filterMap[istioConfigFilter] = configLabels
	}
	return configLabels.Fields
}

// getIstioMetadata returns ????
func (ep *Endpoint) getIstioMetadata() map[string]*types.Value {
	metadata := ep.Metadata
	if metadata == nil {
		return nil
	}
	filterMap := metadata.GetFilterMetadata()
	if filterMap == nil {
		return nil
	}
	configLabels := filterMap[istioConfigFilter]
	if configLabels == nil {
		return nil
	}
	return configLabels.GetFields()
}

func (ep *Endpoint) matchDomainSuffix(domainPattern string) bool {
	for _, epDomainName := range ep.getMultiValuedAttrs(DestinationDomain.AttrName()) {
		if strings.HasSuffix(epDomainName, domainPattern) {
			return true
		}
	}
	return false
}

// deleteEndpoint removes all internal references to the endpoint, i.e  from
// allEndpoints, subsetEndpoints, reverseAttrMap and reverseEpSubsets. This method
// is expected to be called only from inside reconcile(). The caller is expected
// to lock the mesh before calling this method().
func (m *Mesh) deleteEndpoint(uid string, ep *Endpoint) {
	// Remove references from reverseAttrMap
	m.reverseAttrMap.deleteEndpoint(ep)

	// Remove references from reverseEpSubsets and subsetEndpoints
	subsets, subsetsFound := m.reverseEpSubsets[ep]
	if subsetsFound {
		for ssName := range subsets {
			subsetEps, epsFound := m.subsetEndpoints[ssName]
			if epsFound {
				delete(subsetEps, ep)
			}
		}
		delete(m.reverseEpSubsets, ep)
	}
	// Remove entry from allEndpoints
	delete(m.allEndpoints, uid)
}

// deleteEndpoint removes the endpoint UID from all name value keysets
// contained in le. This method is expected to be called only from inside
// reconcile(). The caller is expected to lock the mesh before calling
// this method().
func (le labelEndpoints) deleteEndpoint(ep *Endpoint) {
	for label, value := range ep.getSingleValuedAttrs() {
		epSet := le[label+nameValueSeparator+value]
		delete(epSet, ep)
	}
	for _, value := range ep.getMultiValuedAttrs(DestinationService.AttrName()) {
		le.deleteLabel(DestinationService.AttrName()+nameValueSeparator+value, ep)
	}
	for _, value := range ep.getMultiValuedAttrs(DestinationDomain.AttrName()) {
		le.deleteLabel(DestinationService.AttrName()+nameValueSeparator+value, ep)
	}
}

// addServiceEndpoint adds this uid to the keysets of for all name value
// pairs of attributes and returns label values corresponding to DestinationService.
func (le labelEndpoints) addEndpoint(ep *Endpoint) []string {
	for label, value := range ep.getSingleValuedAttrs() {
		le.addLabel(label+nameValueSeparator+value, ep)
	}
	destServices := ep.getMultiValuedAttrs(DestinationService.AttrName())
	for _, value := range ep.getMultiValuedAttrs(DestinationService.AttrName()) {
		le.addLabel(DestinationService.AttrName()+nameValueSeparator+value, ep)
	}
	for _, value := range ep.getMultiValuedAttrs(DestinationDomain.AttrName()) {
		le.addLabel(DestinationDomain.AttrName()+nameValueSeparator+value, ep)
	}
	return destServices
}

// getEndpointsMatching returns a set of endpoints that match the values
// of all labels with a reasonably predictable performance. It does
// this by fetching the Endpoint sets for for each matching label value
// combination then uses the shortest set for checking whether those
// endpoints are present in the other sets.
func (le labelEndpoints) getEndpointsMatching(labels map[string]string) endpointSet {
	countLabels := len(labels)
	if countLabels == 0 {
		// There must be at least one label else return nothing
		return endpointSet{}
	}
	// Note: 0th index has the smallest endpoint set
	matchingSets := make([]endpointSet, countLabels)
	smallestSetLen := math.MaxInt32
	setIdx := 0
	for l, v := range labels {
		epSet, found := le[l+nameValueSeparator+v]
		if !found {
			// Nothing matched at least one label
			return endpointSet{}
		}
		matchingSets[setIdx] = epSet
		lenKeySet := len(epSet)
		if lenKeySet < smallestSetLen {
			smallestSetLen = lenKeySet
			if setIdx > 0 {
				swappedMatchingSet := matchingSets[0]
				matchingSets[0] = matchingSets[setIdx]
				matchingSets[setIdx] = swappedMatchingSet
			}
		}
		setIdx++
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

// addLabel adds an endpoint to the set of endpoints associated with
// the labelKey which is built from the label and value.
func (le labelEndpoints) addLabel(labelKey string, ep *Endpoint) {
	epSet, found := le[labelKey]
	if !found {
		epSet = make(endpointSet)
		le[labelKey] = epSet
	}
	epSet[ep] = true
}

// deleteLabel removes the endpoint from the set of endpoints associated with
// the labelKey which is built from the label and value.
func (le labelEndpoints) deleteLabel(labelKey string, ep *Endpoint) {
	epSet := le[labelKey]
	delete(epSet, ep)
	if len(epSet) == 0 {
		delete(le, labelKey)
	}
}

// addLabelEndpoints merges other labelEndpoints with le.
func (le labelEndpoints) addLabelEndpoints(other labelEndpoints) {
	for labelKey, otherEpSet := range other {
		epSet, found := le[labelKey]
		if len(epSet) == 0 {
			continue
		}
		if !found {
			epSet = make(endpointSet, len(otherEpSet))
			le[labelKey] = epSet
		}
		for ep := range otherEpSet {
			epSet[ep] = true
		}
	}
}

// newEndpointSet creates a copy of fromSet that can be modified without altering fromSet
func newEndpointSet(fromSet endpointSet) endpointSet {
	out := make(endpointSet, len(fromSet))
	for ep, v := range fromSet {
		if v {
			out[ep] = v
		}
	}
	return out
}

// appendAll merges endpoints from other into eps
func (eps endpointSet) appendAll(other endpointSet) {
	for ep, v := range other {
		if v {
			eps[ep] = v
		}
	}
}

func (eps endpointSet) scopeToRule(ruleType DestinationRuleType, labelValue string, cidrNet *net.IPNet) endpointSet {
	out := make(endpointSet, len(eps))
	switch ruleType {
	case DestinationRuleWildcard:
		for ep := range eps {
			if ep.matchDomainSuffix(labelValue) {
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

// addSubset adds a subset name to an existing set of subset names mapped to the endpoint
func (es endpointSubsets) addSubset(ep *Endpoint, subsetName string) {
	ssNames, found := es[ep]
	if !found {
		ssNames = map[string]bool{}
		es[ep] = ssNames
	}
	ssNames[subsetName] = true
}

// deleteSubset deletes a subset key from the set of subset keys mapped to the endpoint key
func (es endpointSubsets) deleteSubset(ep *Endpoint, subsetName string) {
	ssNames, found := es[ep]
	if !found {
		return
	}
	delete(ssNames, subsetName)
	if len(ssNames) == 0 {
		delete(es, ep)
	}
}

// addEndpoint adds an endpoint to an existing set of endpoint UIDs mapped to the subset key
func (se subsetEndpoints) addEndpoint(subsetName string, ep *Endpoint) {
	epUIDSet, found := se[subsetName]
	if !found {
		epUIDSet = endpointSet{}
		se[subsetName] = epUIDSet
	}
	epUIDSet[ep] = true
}

// addEndpointSet adds a set of endpoints to an existing set of endpoints mapped to the subset name
func (se subsetEndpoints) addEndpointSet(subsetName string, otherEpSet endpointSet) {
	epSet, found := se[subsetName]
	if !found {
		epSet = make(endpointSet, len(otherEpSet))
		se[subsetName] = epSet
	}
	for endpoint := range otherEpSet {
		epSet[endpoint] = true
	}
}

// addSubsetEndpoints merges others with se
func (se subsetEndpoints) addSubsetEndpoints(other subsetEndpoints) {
	for subsetName, epKetSet := range other {
		se.addEndpointSet(subsetName, epKetSet)
	}
}

// AttrName returns the string value of attr.
func (attr DestinationAttribute) AttrName() string {
	return (string)(attr)
}

// logAndReturnReconcileErrors create an error with the prefix relating to Reconcile(), log it  and return the prefixed
func logAndReturnReconcileErrors(err error) error {
	out := multierror.Prefix(err, "errors reconciling controller endpoints with mesh")
	log.Error(out.Error())
	return out
}

// newMapWithLabelValue returns a copy of labels with the name value mapping added to it.
func newMapWithLabelValue(labels map[string]string, name, value string) map[string]string {
	out := make(map[string]string, len(labels)+1)
	out[name] = value
	for n, v := range labels {
		out[n] = v
	}
	return out
}

// init initializes global vars.
func init() {
	prohibitedAttrSet = map[string]bool{
		DestinationIP.AttrName():   true,
		DestinationPort.AttrName(): true,
	}
}
