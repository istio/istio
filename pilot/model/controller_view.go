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

// The ControllerView is a helper object that enables platform registry
// specific Controllers to implement ServiceDiscovery and ServiceAccounts,
// thereby reducing boiler plate code and tests needed by every platform Controller
//
// Typical Usage:
//
//   import "istio.io/istio/pilot/model"
//
//   type MyPlatformController struct {
//     ControllerView
//	 }
//   ...
//   pc := MyPlatformController{model.NewControllerView()}
//   ...
//   var services []*model.Service
//   var serviceInstances []*model.Service
//   services = buildYourPlatformServiceList()
//   serviceInstances = buildYourPlatformServiceInstanceList()
//   pc.Reconcile(services, serviceInstances)

package model

import (
	"encoding/hex"
	"math"
	"net"
	"reflect"
	"sync"
)

const (
	// All labels below are for ControllerView internals only. These labels
	// are by ServiceDiscovery method implementations of ControllerView.

	// Prefix for labels used internally by the ControllerView
	labelPilotPrefix = "config.istio.io/pilot."

	// Prefix for labels used internally by ControllerView for labels pertaining
	// to Service properties
	labelServicePrefix = labelPilotPrefix + "Service."

	// Label used internally by ControllerView to fetch services or instances
	//  by Service Hostname
	labelServiceName = labelServicePrefix + "hostname"

	// Label used internally by ControllerView to fetch services or instances
	// by the DNS name associated for the service
	labelServiceExternalName = labelServicePrefix + "externalName"

	// Label used internally by ControllerView to fetch services or instances
	// by the service VIP. While ServiceDiscovery function parms may contain
	// ipv4/v6 address, example 10.1.1.3, the value associated with
	// this internal label is a normalized IPv6 address encoded in hex
	labelServiceVIP = labelServicePrefix + "vip"

	// Prefix for labels used internally by ControllerView for pertaining to
	// to Service Instance properties
	labelInstancePrefix = labelPilotPrefix + "ServiceInstance."

	// Label used internally by ControllerView to fetch instances
	// by instance IP. While ServiceDiscovery function parms may contain
	// ipv4/v6 address, example 10.1.1.3, the value associated with
	// this internal label is a normalized IPv6 address encoded in hex
	labelInstanceIP = labelInstancePrefix + "ip"

	// Label used internally by ControllerView to fetch instances
	// by Port. While ServiceDiscovery function parms may contain
	// alternate numeric formats for port numbers, ex: 80 or 0080
	// etc, the value associated with this internal label is a
	// normalized 16 bit int encoded in hex
	labelInstancePort = labelInstancePrefix + "port"

	// Label used internally by ControllerView to fetch instances
	// by the named port.
	labelInstanceNamedPort = labelInstancePrefix + "namedPort"
)

// ControllerView implements common storage of mesh entities like Service
// and ServiceInstance objects and is intended to be used by individual
// platform registry Controllers. Controller implementations call Reconcile()
// to update the Controller's view with the most up-to-date list of mesh
// entities available natively for that platform registry.
//
// TODO adopt ControllerView into all platform registry implementations as part
// of https://github.com/istio/istio/issues/1223.
//
// Although objects from each Controller are distinct, they implicitly follow the
// following contraints:
//
// - Service objects: are unique across the mesh for the combination of their
// Hostname and Address
// - ServiceInstance objects: are unique across the mesh for the combination
// of their Service.Hostname and Endpoint.Address and Endpoint.Port
//
// The ControllerView will never modify model.* objects. However to accommodate
// multiple data representations of various properties of model.* objects, the
// ControllerView normalizes the following properties to allow for uniform
// query operations across the mesh:
//
// - Address: Fetches by Service and ServiceInstance addresses are
// always normalized to their 16 byte IPv6 representation.
// - Port: Given that 0080 and 80 are equivalent, port numbers are always normalized
// by their numeric 16 bit value.
type ControllerView struct {

	// serviceLabels holds the inverted map of label name/values to the keys in
	// services for O(N) access involving labels versus O(N^2) access via services
	serviceLabels viewLabels

	// services is a map maintained by the Controller with a composite key of
	// Service.Hostname and Service.Address. The key is expected to be mesh unique
	services map[string]*Service

	// instanceLabels holds the inverted map of label name/values to the keys in
	// serviceInstances for O(N) access involving labels versus O(N^2) access via
	// services
	instanceLabels viewLabels

	// serviceInstances is a map maintained by the Controller with a composite key
	// of Service.Hostname, Endpoint.Address and Endpoint.Port. The key is expected
	// to be mesh unique
	serviceInstances map[string]*ServiceInstance

	// Mutex guards updates to all model.* objects of the ControllerView
	mu sync.RWMutex
}

// viewKeySet is a set of controller view keys. All keys in the
// key set must be of the same type:
//
// - Service keys are of the form [Service.Hostname][16 byte hex formatted hex formatted
//	 IPv6 representation Service.Address]
// - ServiceInstance keys are of the form [Service.Hostname][16 byte hex formatted IPv6
//	 representation of Endpoint.Address][2 byte hex formatted Endpoint.Port]
type viewKeySet map[string]bool

// viewValues holds a map of label values to viewKeySet
type viewValues map[string]viewKeySet

// viewLabels holds a map of label names to viewValues
type viewLabels map[string]viewValues

// NewControllerView creates a new empty ControllerView for use by Controller implementations
func NewControllerView() *ControllerView {
	return &ControllerView{
		serviceLabels:    viewLabels{},
		services:         map[string]*Service{},
		instanceLabels:   viewLabels{},
		serviceInstances: map[string]*ServiceInstance{},
		mu:               sync.RWMutex{},
	}
}

// Reconcile is intended to be called by individual platform registry Controllers to
// update the ControllerView with the latest state of mesh entities that make up the
// view. There should be only one thread calling Reconcile and the services and instances
// passed to Reconcile must represent the complete state of the Controller.
func (cv *ControllerView) Reconcile(svcs []*Service, instances []*ServiceInstance) {
	cv.reconcileServices(svcs)
	cv.reconcileServiceInstances(instances)
}

// Services implements ServiceDiscovery.Services()
func (cv *ControllerView) Services() ([]*Service, error) {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	svcKeySet := cv.serviceLabels.getAllKeysMatchingLabel(labelServiceName)
	out := make([]*Service, len(svcKeySet))
	i := 0
	for k := range svcKeySet {
		out[i] = cv.services[k]
		i++
	}
	return out, nil
}

// GetService implements ServiceDiscovery.GetService()
func (cv *ControllerView) GetService(hostname string) (*Service, error) {
	lbls := Labels{labelServiceName: hostname}
	services := cv.servicesByLabels(lbls)
	if len(services) > 0 {
		return services[0], nil
	}
	return nil, nil
}

// Instances implements ServiceDiscovery.Instances()
func (cv *ControllerView) Instances(hostname string, ports []string, labelCollection LabelsCollection) ([]*ServiceInstance, error) {
	out := []*ServiceInstance{}
	for _, port := range ports {
		if len(labelCollection) > 0 {
			for _, labels := range labelCollection {
				labelsAndHostPort := labels.Copy()
				labelsAndHostPort[labelInstanceNamedPort] = port
				labelsAndHostPort[labelServiceName] = hostname
				out = append(out, cv.serviceInstancesByLabels(labelsAndHostPort)...)
			}
		} else {
			labelsAndHostPort := Labels{
				labelInstanceNamedPort: port,
				labelServiceName:       hostname,
			}
			out = append(out, cv.serviceInstancesByLabels(labelsAndHostPort)...)
		}
	}
	return out, nil
}

// HostInstances implements ServiceDiscovery.HostInstances()
func (cv *ControllerView) HostInstances(addrs map[string]bool) ([]*ServiceInstance, error) {
	out := []*ServiceInstance{}
	for addr := range addrs {
		labels := Labels{labelInstanceIP: getNormalizedIP(addr)}
		out = append(out, cv.serviceInstancesByLabels(labels)...)
	}
	return out, nil
}

// ManagementPorts implements ServiceDiscovery.ManagementPorts()
// TODO - as part of https://github.com/istio/istio/issues/1223
// Management ports should be accommodated in ServiceInstance
// Currently implements
func (cv *ControllerView) ManagementPorts(addr string) PortList {
	// TODO Will be implemented as part of
	// https://github.com/istio/istio/issues/1223
	return PortList{}
}

// GetIstioServiceAccounts implements ServiceAccounts.GetIstioServiceAccounts()
func (cv *ControllerView) GetIstioServiceAccounts(hostname string, ports []string) []string {
	// Empty label set to fetch all instances matching hostname and ports
	labelCollection := LabelsCollection{Labels{}}
	instances, _ := cv.Instances(hostname, ports, labelCollection)
	saSet := make(map[string]bool)
	for _, instance := range instances {
		if instance.ServiceAccount != "" {
			saSet[instance.ServiceAccount] = true
		}
		for _, svcAcct := range instance.Service.ServiceAccounts {
			if svcAcct != "" {
				saSet[svcAcct] = true
			}
		}
	}
	out := make([]string, len(saSet))
	idx := 0
	for acct := range saSet {
		out[idx] = acct
		idx++
	}
	return out
}

// servicesByLabels returns a list of Service objects that match all supplied
// labels
func (cv *ControllerView) servicesByLabels(labels Labels) []*Service {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	svcKeySet := cv.serviceLabels.getKeysMatching(labels)
	out := make([]*Service, len(svcKeySet))
	i := 0
	for k := range svcKeySet {
		out[i] = cv.services[k]
		i++
	}
	return out
}

// serviceInstancesByLabels returns a list of ServiceInstance objects that match all
// supplied labels
func (cv *ControllerView) serviceInstancesByLabels(labels Labels) []*ServiceInstance {
	cv.mu.RLock()
	defer cv.mu.RUnlock()
	instKeySet := cv.instanceLabels.getKeysMatching(labels)
	out := make([]*ServiceInstance, len(instKeySet))
	i := 0
	for k := range instKeySet {
		out[i] = cv.serviceInstances[k]
		i++
	}
	return out
}

// Reconciles services in the ControllerView with the list of expectedServices
func (cv *ControllerView) reconcileServices(expectedServices []*Service) {
	// No need to synchronize cv.services given this is the only writer thread
	// All modifications to cv ought to be done after locking cv.mu
	actualKeySvcMap := make(map[string]*Service, len(cv.services))
	for k := range cv.services {
		actualKeySvcMap[k] = cv.services[k]
	}

	expectedKeySvcMap := make(map[string]*Service, len(expectedServices))
	for _, svc := range expectedServices {
		svcKey := "[" + svc.Hostname + "]"
		if svc.Address != "" {
			svcKey = svcKey + "[" + getNormalizedIP(svc.Address) + "]"
		}
		expectedKeySvcMap[svcKey] = svc
	}
	updateSet := map[string]*Service{}
	for k, expSvc := range expectedKeySvcMap {
		actSvc, found := actualKeySvcMap[k]
		if !found {
			continue // Needs to be added to ControllerView
		}
		if !reflect.DeepEqual(*expSvc, *actSvc) {
			updateSet[k] = expSvc
		}
		// Remaining would be ones that need adding
		delete(expectedKeySvcMap, k)
		// Remaining would be ones that need deleting
		delete(actualKeySvcMap, k)
	}
	cv.mu.Lock()
	defer cv.mu.Unlock()
	for k, delSvc := range actualKeySvcMap {
		cv.reconcileService(k, delSvc, EventDelete)
	}
	for k, addSvc := range expectedKeySvcMap {
		cv.reconcileService(k, addSvc, EventAdd)
	}
	for k, updSvc := range updateSet {
		cv.reconcileService(k, updSvc, EventUpdate)
	}
}

// Reconciles service instances in the ControllerView with the list of expectedInstances
func (cv *ControllerView) reconcileServiceInstances(expectedInstances []*ServiceInstance) {
	// No need to synchronize cv.services given this is the only writer thread
	// All modifications to cv ought to be done after locking cv.mu
	actualKeyInstMap := make(map[string]*ServiceInstance, len(cv.serviceInstances))
	for k := range cv.serviceInstances {
		actualKeyInstMap[k] = cv.serviceInstances[k]
	}

	expectedKeyInstMap := make(map[string]*ServiceInstance, len(expectedInstances))
	for _, inst := range expectedInstances {
		instKey := "[" + inst.Service.Hostname + "][" + getNormalizedIP(inst.Endpoint.Address) + "][" +
			getNormalizedPort(inst.Endpoint.Port)
		expectedKeyInstMap[instKey] = inst
	}
	updateSet := map[string]*ServiceInstance{}
	for k, expInst := range expectedKeyInstMap {
		actInst, found := actualKeyInstMap[k]
		if !found {
			continue // Needs to be added to ControllerView
		}
		if !reflect.DeepEqual(*expInst, *actInst) {
			updateSet[k] = expInst
		}
		// Remaining would be ones that need adding
		delete(expectedKeyInstMap, k)
		// Remaining would be ones that need deleting
		delete(actualKeyInstMap, k)
	}
	cv.mu.Lock()
	defer cv.mu.Unlock()
	for k, delInst := range actualKeyInstMap {
		cv.reconcileServiceInstance(k, delInst, EventDelete)
	}
	for k, addInst := range expectedKeyInstMap {
		cv.reconcileServiceInstance(k, addInst, EventAdd)
	}
	for k, updInst := range updateSet {
		cv.reconcileServiceInstance(k, updInst, EventUpdate)
	}
}

// reconcileService is expected to be called only from inside reconcileServices(). The caller is expected
// to lock the resource view before calling this method()
func (cv *ControllerView) reconcileService(k string, s *Service, e Event) {
	old, found := cv.services[k]
	if found {
		cv.serviceLabels.deleteLabel(k, labelServiceName, old.Hostname)
		if old.ExternalName != "" {
			cv.serviceLabels.deleteLabel(k, labelServiceExternalName, old.ExternalName)
		}
		if old.Address != "" {
			cv.serviceLabels.deleteLabel(k, labelServiceVIP, getNormalizedIP(old.Address))
		}
		delete(cv.services, k)
	}
	if e != EventDelete {
		// Treat as upsert
		cv.serviceLabels.addLabel(k, labelServiceName, s.Hostname)
		if s.ExternalName != "" {
			cv.serviceLabels.addLabel(k, labelServiceExternalName, s.ExternalName)
		}
		if s.Address != "" {
			cv.serviceLabels.addLabel(k, labelServiceVIP, getNormalizedIP(s.Address))
		}
		cv.services[k] = s
	}
}

// reconcileServiceInstance is expected to be called only from inside reconcileServiceInstances(). The caller is expected
// to lock the resource view before calling this method()
func (cv *ControllerView) reconcileServiceInstance(k string, i *ServiceInstance, e Event) {
	old, found := cv.serviceInstances[k]
	if found {
		cv.instanceLabels.deleteLabel(k, labelServiceName, old.Service.Hostname)
		cv.instanceLabels.deleteLabel(k, labelInstanceIP, getNormalizedIP(old.Endpoint.Address))
		cv.instanceLabels.deleteLabel(k, labelInstancePort, getNormalizedPort(old.Endpoint.Port))
		if old.Endpoint.ServicePort.Name != "" {
			cv.instanceLabels.deleteLabel(k, labelInstanceNamedPort, old.Endpoint.ServicePort.Name)
		}
		for label, value := range old.Labels {
			cv.instanceLabels.deleteLabel(k, label, value)
		}
		delete(cv.serviceInstances, k)
	}
	if e != EventDelete {
		// Treat as upsert
		cv.instanceLabels.addLabel(k, labelServiceName, i.Service.Hostname)
		cv.instanceLabels.addLabel(k, labelInstanceIP, getNormalizedIP(i.Endpoint.Address))
		cv.instanceLabels.addLabel(k, labelInstancePort, getNormalizedPort(i.Endpoint.Port))
		if i.Endpoint.ServicePort != nil && i.Endpoint.ServicePort.Name != "" {
			cv.instanceLabels.addLabel(k, labelInstanceNamedPort, i.Endpoint.ServicePort.Name)
		}
		for label, value := range i.Labels {
			cv.instanceLabels.addLabel(k, label, value)
		}
		cv.serviceInstances[k] = i
	}
}

// Given a set of labels, gets a set of keys that match the values
// of all labels
func (vl viewLabels) getKeysMatching(labels Labels) viewKeySet {
	type matchingSet struct {
		labelName  string
		labelValue string
		keySet     viewKeySet
	}
	countLabels := len(labels)
	if countLabels == 0 {
		// There must be at least one label else return nothing
		return viewKeySet{}
	}
	// Note: 0th index has the smallest keySet
	matchingSets := make([]matchingSet, countLabels)
	smallestSetLen := math.MaxInt32
	setIdx := 0
	for l, v := range labels {
		valueKeysetMap := vl[l]
		if len(valueKeysetMap) == 0 {
			// Nothing matched at least one label name
			return viewKeySet{}
		}
		valKeySet := valueKeysetMap[v]
		lenKeySet := len(valKeySet)
		if lenKeySet == 0 {
			// There were no service keys for this label value
			return viewKeySet{}
		}
		matchingSets[setIdx].keySet = valKeySet
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
		return matchingSets[0].keySet
	}
	out := newViewKeySet(matchingSets[0].keySet)
	for k := range out {
		for setIdx := 1; setIdx < countLabels; setIdx++ {
			_, found := matchingSets[setIdx].keySet[k]
			if !found {
				delete(out, k)
				break
			}
		}
	}
	return out
}

// Given a label name, fetches a set of keys that have the label,
// irrespective of what the label value may be.
func (vl viewLabels) getAllKeysMatchingLabel(l string) viewKeySet {
	valueKeysetMap := vl[l]
	if len(valueKeysetMap) == 0 {
		// Nothing matched at least one label name
		return viewKeySet{}
	}
	out := viewKeySet{}
	for _, keySet := range valueKeysetMap {
		out.appendAll(keySet)
	}
	return out
}

// addLabel creates the reverse lookup by label name and label value for the key k.
func (vl viewLabels) addLabel(k, labelName, labelValue string) {
	valueKeysetMap, labelNameFound := vl[labelName]
	if !labelNameFound {
		valueKeysetMap = make(viewValues)
		vl[labelName] = valueKeysetMap
	}
	keySet, labelValueFound := valueKeysetMap[labelValue]
	if !labelValueFound {
		keySet = make(viewKeySet)
		valueKeysetMap[labelValue] = keySet
	}
	keySet[k] = true
}

// deleteLabel removes the key k from the reverse lookup of the label name and value
func (vl viewLabels) deleteLabel(k string, labelName, labelValue string) {
	valueKeysetMap, labelNameFound := vl[labelName]
	if !labelNameFound {
		return
	}
	keySet, labelValueFound := valueKeysetMap[labelValue]
	if !labelValueFound {
		return
	}
	delete(keySet, k)
	if len(keySet) > 0 {
		return
	}
	delete(valueKeysetMap, labelValue)
	if len(valueKeysetMap) > 0 {
		return
	}
	delete(vl, labelName)
}

// newKeySet creates a copy of fromKs that can be modified without altering fromKs
func newViewKeySet(fromKs viewKeySet) viewKeySet {
	out := make(viewKeySet, len(fromKs))
	for k, v := range fromKs {
		if v {
			out[k] = v
		}
	}
	return out
}

// appendAll appends all keys from fromKs to ks
func (ks viewKeySet) appendAll(fromKs viewKeySet) {
	for k, v := range fromKs {
		if v {
			ks[k] = v
		}
	}
}

// getNormalizedIP returns a normalized IPv6 addresses encoded in hex
// given an IPv4 or IPv6 address
func getNormalizedIP(address string) string {
	ip := net.ParseIP(address)
	return hex.EncodeToString(ip)
}

// getNormalizedPort returns a normalized 16 bit port number encoded in hex
func getNormalizedPort(port int) string {
	pb := []byte{byte((port >> 8) & 0xFF), byte(port & 0xFF)}
	return hex.EncodeToString(pb)
}
