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
//   meshEndpoints := make([]*Endpoint, len(nativeEndpoints))
//	 for idx, nativeEp := range nativeEndpoints {
//     // Create mesh endpoint from relevant values of nativeEp
//	   meshEndpoints[idx] = NewEndpoint(......)
//   }
//   pc.Reconcile(meshEndpoints)
//
package model

import (
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"sync"

	xdsapi "github.com/envoyproxy/go-control-plane/api"
	types "github.com/gogo/protobuf/types"
	multierror "github.com/hashicorp/go-multierror"
	route "istio.io/api/routing/v1alpha2"
	"istio.io/istio/pkg/log"
)

const (
	// The internal filter name for attributes that should be used for
	// constructing Istio attributes on the Endpoint in
	// <a href='https://goo.gl/mFzjpp'>reverse DNS form</a>.
	istioConfigFilter = "io.istio.istio-config"

	// Key Istio attribute names for mapping endpoints to subsets.
	// For details, please see
	// https://istio.io/docs/reference/config/mixer/attribute-vocabulary.html

	// UID is the attribute name for the mesh unique, platform-specific
	// unique identifier for the server instance of the destination service. Mesh
	// uses the label to uqniquely identify the endpoint when it receives updates
	// from the Controller. Example: kubernetes://my-svc-234443-5sffe.my-namespace
	// No two instances running anywhere in the mesh can have the same value for
	// UID.
	UID destinationAttribute = "destination.uid"

	// SERVICE represents the fully qualified __CANONOCAL__ name of the Istio
	// service, ex: "my-svc.my-namespace.svc.cluster.local".  Mesh uses
	// this label to identify endpoints belonging to the same service for XDS queries
	// involving service names. The attribute value is expected to be mesh unique,
	// meaning services having this name are semantically understood to be the same
	// modulo version and other labels, irrespective of the platform under which
	// they are run, the clusters or availability zones they run in.
	SERVICE destinationAttribute = "destination.service"

	// NAME represents the short part of the service name, ex: "my-svc".
	NAME destinationAttribute = "destination.name"

	// NAMESPACE represents the namespace part of the destination service,
	// example: "my-namespace".
	NAMESPACE destinationAttribute = "destination.namespace"

	// IP represents the IP address of the server instance, example 10.0.0.104.
	// This IP is expected to be reachable from __THIS__ pilot. No distinction
	// is being made for directly reachable service instances versus those
	// behind a VIP. Istio's health discovery service will ensure that this
	// endpoint's capacity is correctly reported accordingly.
	IP destinationAttribute = "destination.ip"

	// PORT represents the recipient port on the server IP address, Example: 443
	PORT destinationAttribute = "destination.port"

	// DOMAIN represents the domain portion of the service name, excluding
	// the name and namespace, example: svc.cluster.local
	DOMAIN destinationAttribute = "destination.domain"

	// USER represents the __CANONICAL__ Mesh user running the destination
	// application, example: service-account-foo. The user's identity is
	// assumed to be trusted within the entire mesh, irrespective of which
	// platform, cluster or availability zone this instance runs in.
	USER destinationAttribute = "destination.user"
)

var (
	destinationAttrSet = map[string]bool{
		UID.stringValue():       true,
		SERVICE.stringValue():   true,
		NAME.stringValue():      true,
		NAMESPACE.stringValue(): true,
		IP.stringValue():        true,
		PORT.stringValue():      true,
		DOMAIN.stringValue():    true,
		USER.stringValue():      true,
	}

	canonicalIstioDomain = "svc.cluster.local"
	svcRegexp            = regexp.MustCompile(
		"([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\\-]*[a-zA-Z0-9])\\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\\-]*[a-zA-Z0-9])\\.svc\\.cluster\\.local")
	domainRegexp = regexp.MustCompile(
		"([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\\-]*[a-zA-Z0-9])\\.([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\\-]*[a-zA-Z0-9])*")
)

type destinationAttribute string

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

	// subsetDefinitions holds the metadata, basically the set of lable name values
	// that ought to match corresponding attributes of the endpoint.
	// The actual subset of allEndpoints that match these subset definitions is
	// stored in subsetEndpoints. Mesh uses this map for identifying config changes
	// to the subset's metadata. The key of this map is the Subset name.
	subsetDefinitions map[string]*route.Subset

	// subsetEndpoints allows for look-ups from the Subset name to a set of Endpoint UIDs.
	// The UIDs in the set are guaranteed to exist in allEndpoints and the
	// corresponding LbEndpointis guaranteed to satisfy the labels in the corresponding
	// Subset.
	subsetEndpoints subsetEndpoints

	// reverseAttrMap provides reverse lookup from label name > label value >
	// endpointUIDSet. All Endpoints in the set are guaranteed to match the
	// attribute with the label name and value. Mesh uses the map for quickly
	// updating subsetEndpoints in response to subset label changes.
	reverseAttrMap attributeValues

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

// endpointUIDSet is a unique set of Endpoint UIDs.
type endpointUIDSet map[string]bool

// valueEndpointSet associates a label value with an endpointUIDSet.
type valueEndpointSet map[string]endpointUIDSet

// attributeValues is a reverse lookup map from label name > label value > endpointUIDs
// that all have the same attribute matching the label name and label value.
type attributeValues map[string]valueEndpointSet

// subsetNameSet is a unique set of subset names.
type subsetNameSet map[string]bool

// endpointSubsets maps an endpoint UID to a set of matching subsets.
type endpointSubsets map[string]subsetNameSet

// subsetEndpoints maps an subset name to a set of matching endpoint UIDs.
type subsetEndpoints map[string]endpointUIDSet

// SubsetEvent is a pilot configuration event for notifying the specified
// Subset is to be added, updated or deleted from Pilot depending on the
// specified Event.
type SubsetEvent struct {
	Subset *route.Subset
	Event  Event
}

// NewMesh creates a new empty Mesh for use by Controller implementations
func NewMesh() *Mesh {
	return &Mesh{
		allEndpoints:      map[string]*Endpoint{},
		subsetDefinitions: map[string]*route.Subset{},
		subsetEndpoints:   subsetEndpoints{},
		reverseAttrMap:    attributeValues{},
		reverseEpSubsets:  endpointSubsets{},
		mu:                sync.RWMutex{},
	}
}

// Reconcile is intended to be called by individual platform Controllers to
// update the Mesh with the latest list of endpoints that make up the
// view into it's associated service registry. There should be only one Controller
// thread calling Reconcile and the endpoints passed to Reconcile must represent
// the complete set of endpoints retrieved for that platform's service registry.
func (m *Mesh) Reconcile(endpoints []*Endpoint) error {
	var errs error
	// Start out with everything that's provided by the controller and only retain
	// what's not currently in m.
	epsToAdd := make(map[string]*Endpoint, len(endpoints))
	for _, ep := range endpoints {
		epUID, found := ep.getLabelValue(UID.stringValue())
		if !found {
			errs = multierror.Append(errs, errors.New(fmt.Sprintf("mandatory attribute '%s' missing in endpoint metadata '%v'", UID.stringValue(), ep)))
			continue
		}
		_, found = ep.getLabelValue(SERVICE.stringValue())
		if !found {
			errs = multierror.Append(errs, errors.New(fmt.Sprintf("mandatory attribute '%s' missing in endpoint metadata '%v'", SERVICE.stringValue(), ep)))
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
	newLabelMappings := attributeValues{}
	newSubsetMappings := subsetEndpoints{}
	for uid, addEp := range epsToAdd {
		newLabelMappings.addServiceEndpoint(uid, addEp)
		m.allEndpoints[uid] = addEp
		// By default we create a subset with the same name as the service.
		// This provides a set of subsets that do not have any bearings on endpoint
		// labels and is used for CDS.
		// TODO: No effort is being made to ensure Subset names are distinct from
		// service names. If this is deemed necessary, one option is to create a unique
		// service-subsetname key. Alternatively galley can create subsets with the name
		// of the service and empty labels. In any case, populating these default
		// subsets may be OK if pilot should not need to wait for configs supplied by
		// galley, given that controllers may already provide Mesh with this info.
		svcName, _ := addEp.getLabelValue(SERVICE.stringValue())
		newSubsetMappings.addEndpointUID(svcName, uid)
	}
	// Verify if existing subset definitions apply for the newly added labels
	for ssName, subset := range m.subsetDefinitions {
		matchingEpKeys := newLabelMappings.getKeysMatching(subset.Labels)
		newSubsetMappings.addEndpointUIDs(ssName, matchingEpKeys)
	}
	// Update mesh for new label mappings and new subset mappings
	m.reverseAttrMap.addAttributeValues(newLabelMappings)
	m.subsetEndpoints.addSubsetEndpoints(newSubsetMappings)
	return nil
}

// MeshEndpoints implements functionality required for EDS and returns a list of endpoints
// that match one or more subsets.
func (m *Mesh) MeshEndpoints(subsetNames []string) []*Endpoint {
	m.mu.RLock()
	defer m.mu.RUnlock()
	epSet := endpointUIDSet{}
	for _, ssName := range subsetNames {
		ssEpSet, found := m.subsetEndpoints[ssName]
		if !found {
			continue
		}
		epSet.appendAll(ssEpSet)
	}
	out := make([]*Endpoint, len(epSet))
	idx := 0
	for uid, _ := range epSet {
		out[idx] = m.allEndpoints[uid]
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

// UpdateSubsets implements functionality required for pilot configuration (via Galley).
// It updates the Mesh for supplied events, adding, updating and deleting Subsets
// from this mesh as determined by the corresponding Event. The list of subsetEvents
// can be a partial one.
func (m *Mesh) UpdateSubsets(subsetEvents []SubsetEvent) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, ssEvent := range subsetEvents {
		subset := ssEvent.Subset
		if ssEvent.Event == EventDelete || ssEvent.Event == EventUpdate {
			ssName := subset.Name
			oldEpKeySet := m.subsetEndpoints[ssName]
			for epKey := range oldEpKeySet {
				m.reverseEpSubsets.deleteSubset(epKey, ssName)
			}
			delete(m.subsetEndpoints, ssName)
		}
		if ssEvent.Event == EventAdd || ssEvent.Event == EventUpdate {
			ssName := subset.Name
			epUIDSet := m.reverseAttrMap.getKeysMatching(subset.Labels)
			m.subsetEndpoints.addEndpointUIDs(ssName, epUIDSet)
			for endpointUID := range epUIDSet {
				m.reverseEpSubsets.addSubset(endpointUID, ssName)
			}
		}
	}
}

// NewEndpoint is a boiler plate function intended for platform Controllers to create a new Endpoint.
// This method ensures all the necessary data required for creating subsets are correctly setup. It
// also performs sorting of arrays etc, to allow stable results for reflect.DeepEquals() for quick
// comparisons.
func NewEndpoint(endpointUID, service string, aliases []string, address string, port uint32, protocol Protocol, labels Labels, user string) (*Endpoint, error) {
	var errs error
	ipAddr := net.ParseIP(address)
	if ipAddr == nil {
		errs = multierror.Append(errs, errors.New(fmt.Sprintf("invalid IP address '%s'", address)))
	}
	svcParts := svcRegexp.FindStringSubmatch(service)
	if svcParts == nil || len(svcParts) != 3 {
		errs = multierror.Append(
			errors.New(fmt.Sprintf(
				"invalid service name format, expecting form <name>.<namespace>.svc.cluster.local found'%s'",
				service)))
	}
	for _, alias := range aliases {
		if !domainRegexp.MatchString(alias) {
			errs = multierror.Append(errors.New(
				fmt.Sprintf("invalid service alias format for service '%s', expecting format '%s' found'%s'",
					service, domainRegexp.String(), alias)))
		}
	}
	destLabels := make(Labels, len(labels)+len(destinationAttrSet))
	for labelName, labelValue := range labels {
		if isDestinationAttribute(labelName) {
			errs = multierror.Append(errors.New(
				fmt.Sprintf("prohibited use of Istio destination label '%s' for endpoint label", labelName)))
		}
		destLabels[labelName] = labelValue
	}
	if errs != nil {
		return nil, multierror.Prefix(errs, "Mesh endpoint creation errors")
	}
	socketProtocol := xdsapi.SocketAddress_TCP
	switch protocol {
	case ProtocolTCP, ProtocolHTTP, ProtocolHTTP2, ProtocolHTTPS, ProtocolRedis, ProtocolMongo, ProtocolGRPC:
		socketProtocol = xdsapi.SocketAddress_TCP
	case ProtocolUDP:
		socketProtocol = xdsapi.SocketAddress_UDP
	}
	ep := Endpoint{
		Endpoint: &xdsapi.Endpoint{
			Address: &xdsapi.Address{
				Address: &xdsapi.Address_SocketAddress{
					&xdsapi.SocketAddress{
						Address:    address,
						Ipv4Compat: ipAddr.To4() != nil,
						Protocol:   socketProtocol,
						PortSpecifier: &xdsapi.SocketAddress_PortValue{
							PortValue: port,
						},
					},
				},
			},
		},
	}

	// Populate Istio destination labels.
	destLabels[UID.stringValue()] = endpointUID
	destLabels[SERVICE.stringValue()] = service
	destLabels[NAMESPACE.stringValue()] = svcParts[1]
	destLabels[DOMAIN.stringValue()] = canonicalIstioDomain
	destLabels[IP.stringValue()] = ipAddr.String()
	destLabels[PORT.stringValue()] = strconv.Itoa((int)(port))
	destLabels[USER.stringValue()] = user
	ep.setLabels(destLabels)

	// Populate destination labels pertaining to alias
	serviceNames := make([]string, len(aliases)+1)
	copy(serviceNames, aliases)
	serviceNames[len(serviceNames)-1] = svcParts[0]
	// Sort for stable comparisons down the line.
	sort.Strings(serviceNames)
	ep.setLabelValues(NAME.stringValue(), serviceNames)

	return &ep, nil
}

// getLabels gets Endpoint labels relevant to subset mapping only.
func (ep *Endpoint) getLabels() Labels {
	out := Labels{}
	metadata := ep.Metadata
	if metadata == nil {
		return out
	}
	filterMap := metadata.GetFilterMetadata()
	if filterMap == nil {
		return out
	}
	configLabels := filterMap[istioConfigFilter]
	if configLabels == nil {
		return out
	}
	for attrName, attrValue := range configLabels.GetFields() {
		labelValue := attrValue.GetStringValue()
		if labelValue == "" {
			continue
		}
		out[attrName] = labelValue
	}
	return out
}

// getLabelValue returns the label value matching attrName and true
// if the endpoint has the supplied attrName. Otherwise this method
// returns an empty string and false.
func (ep *Endpoint) getLabelValue(attrName string) (string, bool) {
	metadata := ep.Metadata
	if metadata == nil {
		return "", false
	}
	filterMap := metadata.GetFilterMetadata()
	if filterMap == nil {
		return "", false
	}
	configLabels := filterMap[istioConfigFilter]
	if configLabels == nil {
		return "", false
	}
	value := configLabels.GetFields()[attrName]
	if value == nil {
		return "", false
	}
	labelValue := value.GetStringValue()
	if labelValue == "" {
		return "", false
	}
	return labelValue, true
}

// setLabelValues allows multiple values to be set for a given attribute.
// Currently used internally for service name and aliases only.
func (ep *Endpoint) setLabelValues(attrName string, attrValues []string) {
	istioMeta := ep.getIstioMetadata()
	listValues := make([]*types.Value, len(attrValues))
	for idx, attrValue := range attrValues {
		listValues[idx] = &types.Value{&types.Value_StringValue{attrValue}}
	}
	istioMeta[attrName] = &types.Value{&types.Value_ListValue{&types.ListValue{listValues}}}
}

// String returns the name of destination attribute.
func (attr destinationAttribute) stringValue() string {
	return (string)(attr)
}

// isDestinationAttribute returns true if the supplied attr is
// one of Istio's destination labels.
func isDestinationAttribute(attr string) bool {
	_, found := destinationAttrSet[attr]
	return found
}

// internalSetLabels is an internally by Mesh to set labels, asserting as
// required whether the label set is allowed to contain destination labels
// or not.
func (ep *Endpoint) setLabels(labels Labels) {
	istioMeta := ep.getIstioMetadata()
	for k, v := range labels {
		istioMeta[k] = &types.Value{
			&types.Value_StringValue{v},
		}
	}
}

func (ep *Endpoint) getIstioMetadata() map[string]*types.Value {
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

// deleteEndpoint removes all internal references to the endpoint, i.e  from
// allEndpoints, subsetEndpoints, reverseAttrMap and reverseEpSubsets. This method
// is expected to be called only from inside reconcile(). The caller is expected
// to lock the mesh before calling this method().
func (m *Mesh) deleteEndpoint(uid string, ep *Endpoint) {
	// Remove references from reverseAttrMap
	m.reverseAttrMap.deleteEndpoint(uid, ep)

	// Remove references from reverseEpSubsets and subsetEndpoints
	subsets, subsetsFound := m.reverseEpSubsets[uid]
	if subsetsFound {
		for ssName := range subsets {
			subsetEps, epsFound := m.subsetEndpoints[ssName]
			if epsFound {
				delete(subsetEps, uid)
			}
		}
		delete(m.reverseEpSubsets, uid)
	}
	// Remove entry from allEndpoints
	delete(m.allEndpoints, uid)
}

// deleteEndpoint removes the endpoint UID from all name value keysets
// contained in av. This method is expected to be called only from inside
// reconcile(). The caller is expected to lock the mesh before calling
// this method().
func (av attributeValues) deleteEndpoint(uid string, ep *Endpoint) {
	for label, value := range ep.getLabels() {
		av.deleteLabel(uid, label, value)
	}
}

// addServiceEndpoint adds this uid to the keysets of for all name value
// pairs of attributes.
func (av attributeValues) addServiceEndpoint(uid string, ep *Endpoint) {
	for label, value := range ep.getLabels() {
		av.addLabel(uid, label, value)
	}
}

// getKeysMatching returns a set of endpointUIDs that match the values
// of all labels with a reasonably predictable performance. It does
// this by fetching the Endpoint key sets for for each matching label
// and value then uses the shortest key set for checking whether the
// keys are present in the other key sets.
func (av attributeValues) getKeysMatching(labels Labels) endpointUIDSet {
	countLabels := len(labels)
	if countLabels == 0 {
		// There must be at least one label else return nothing
		return endpointUIDSet{}
	}
	// Note: 0th index has the smallest keySet
	matchingSets := make([]endpointUIDSet, countLabels)
	smallestSetLen := math.MaxInt32
	setIdx := 0
	for l, v := range labels {
		valKeySetMap, found := av[l]
		if !found {
			// Nothing matched at least one label name
			return endpointUIDSet{}
		}
		epUIDSet, found := valKeySetMap[v]
		if !found {
			// There were no service keys for this label value
			return endpointUIDSet{}
		}
		matchingSets[setIdx] = epUIDSet
		lenKeySet := len(epUIDSet)
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
	out := newEndpointUIDSet(matchingSets[0])
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

// addLabel creates the reverse lookup by label name and label value for the key uid.
func (av attributeValues) addLabel(uid, labelName, labelValue string) {
	valKeySet, labelNameFound := av[labelName]
	if !labelNameFound {
		valKeySet = make(valueEndpointSet)
		av[labelName] = valKeySet
	}
	keySet, labelValueFound := valKeySet[labelValue]
	if !labelValueFound {
		keySet = make(endpointUIDSet)
		valKeySet[labelValue] = keySet
	}
	keySet[uid] = true
}

// addAttributeValues merges other attributeValues with av.
func (av attributeValues) addAttributeValues(other attributeValues) {
	for labelName, othervalueEndpointSet := range other {
		valKeySet, labelNameFound := av[labelName]
		if !labelNameFound {
			valKeySet = make(valueEndpointSet)
			av[labelName] = valKeySet
		}
		for labelValue, otherKeySet := range othervalueEndpointSet {
			keySet, labelValueFound := valKeySet[labelValue]
			if !labelValueFound {
				keySet = make(endpointUIDSet)
				valKeySet[labelValue] = keySet
			}
			for uid := range otherKeySet {
				keySet[uid] = true
			}
		}
	}
}

// deleteLabel removes the key uid from the reverse lookup of the label name and value
func (av attributeValues) deleteLabel(uid string, labelName, labelValue string) {
	valKeySet, labelNameFound := av[labelName]
	if !labelNameFound {
		return
	}
	keySet, labelValueFound := valKeySet[labelValue]
	if !labelValueFound {
		return
	}
	delete(keySet, uid)
	if len(keySet) > 0 {
		return
	}
	delete(valKeySet, labelValue)
	if len(valKeySet) > 0 {
		return
	}
	delete(av, labelName)
}

// newEndpointUIDSet creates a copy of fromSet that can be modified without altering fromSet
func newEndpointUIDSet(fromSet endpointUIDSet) endpointUIDSet {
	out := make(endpointUIDSet, len(fromSet))
	for uid, v := range fromSet {
		if v {
			out[uid] = v
		}
	}
	return out
}

// appendAll merges UIDs from other into eps
func (eps endpointUIDSet) appendAll(other endpointUIDSet) {
	for uid, v := range other {
		eps[uid] = v
	}
}

// addSubset adds a subset name to an existing set of subset names mapped to the endpoint UID
func (es endpointSubsets) addSubset(endpointUID, subsetName string) {
	ssNames, found := es[endpointUID]
	if !found {
		ssNames = subsetNameSet{}
		es[endpointUID] = ssNames
	}
	ssNames[subsetName] = true
}

// deleteSubset deletes a subset key from the set of subset keys mapped to the endpoint key
func (es endpointSubsets) deleteSubset(endpointUID, subsetName string) {
	ssNames, found := es[endpointUID]
	if !found {
		return
	}
	delete(ssNames, subsetName)
	if len(ssNames) == 0 {
		delete(es, endpointUID)
	}
}

// addEndpointUID adds an endpoint UID to an existing set of endpoint UIDs mapped to the subset key
func (se subsetEndpoints) addEndpointUID(subsetName, endpointUID string) {
	epUIDSet, found := se[subsetName]
	if !found {
		epUIDSet = endpointUIDSet{}
		se[subsetName] = epUIDSet
	}
	epUIDSet[endpointUID] = true
}

// addEndpointUIDs adds a set of endpoint keys to an existing set of endpoint keys mapped to the subset key
func (se subsetEndpoints) addEndpointUIDs(subsetName string, epUIDSet endpointUIDSet) {
	epUIDSet, found := se[subsetName]
	if !found {
		epUIDSet = endpointUIDSet{}
		se[subsetName] = epUIDSet
	}
	for endpointUID := range epUIDSet {
		epUIDSet[endpointUID] = true
	}
}

// addSubsetEndpoints merges others with se
func (se subsetEndpoints) addSubsetEndpoints(other subsetEndpoints) {
	for subsetName, epKetSet := range other {
		se.addEndpointUIDs(subsetName, epKetSet)
	}
}

// logAndReturnReconcileErrors create an error with the prefix relating to Reconcile(), log it  and return the prefixed
func logAndReturnReconcileErrors(err error) error {
	out := multierror.Prefix(err, "errors reconciling controller endpoints with mesh")
	log.Error(out.Error())
	return out
}
