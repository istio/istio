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

package aggregate

import (
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/golang/glog"

	"istio.io/istio/pilot/model"
	"istio.io/istio/pilot/platform"
)

const (
	// See labelsFromModel() for more on why we need
	// internal / external representations
	labelXdsPrefix     = "config.istio.io/xds"

	labelControllerPath = labelXdsPrefix + "ControllerPath"

	labelServicePrefix = labelXdsPrefix + "Service."
	// xDS external interface for host / service name
	labelServiceName = labelServicePrefix + "name"
	// xDS external interface for dns name
	labelServiceExternalName = labelServicePrefix + "externalName"
	// xDS external interface for service VIP
	labelServiceVIP     = labelServicePrefix + "vip"
	labelInstancePrefix = labelXdsPrefix + "ServiceInstance."
	// xDS external interface for the instance IP.
	// Exernal format is a valid ipv4/v6 string format, ex: 10.1.1.3
	// Internally stored format is a hex representation
	labelInstanceIP = labelInstancePrefix + "ip"
	// xDS external interface for instance Port
	// Exernal format is a valid 16 bit unsigned integer
	// Internally stored format is a hex representation
	labelInstancePort      = labelInstancePrefix + "port"
	labelInstanceNamedPort = labelInstancePrefix + "namedPort"
	
	// Value is Service Name + ip + port
	labelInstanceRef 	   = labelInstancePrefix + "reference"
)

// Registry specifies the collection of service registry related interfaces
type Registry struct {
	Name platform.ServiceRegistry
	model.Controller
	// The service mesh view that this registry belongs to
	MeshView *MeshResourceView
}

// MeshResourceView is an aggregated store for resources sourced from various registries
type MeshResourceView struct {
	registries []Registry

	// Mutex guards services, serviceInstances and corresponding labels
	mu sync.RWMutex

	// Canonical map of mesh service key to service references
	services map[resourceKey]*model.Service

	// Canonical map of mesh service instance keys to service instance references
	serviceInstances map[resourceKey]*model.ServiceInstance

	// A reverse map that associates label names to label values and their associated service resource keys
	serviceLabels nameValueKeysMap

	// A reverse map that associates label names to label values and their associated service instance resource keys
	serviceInstanceLabels nameValueKeysMap

	// Cache eviction handler for notifying changes to model objects
	cacheEvictionHandler model.CacheEvictionHandler
}

type serviceList []*model.Service

func  (sl *serviceList) String() string {
    out := "["
    for _, s := range *sl {
        out = out + s.Hostname + ", "
    }
    out = out + "]"
    return out
}

type serviceInstanceList []*model.ServiceInstance

func  (sl *serviceInstanceList) String() string {
    out := "["
    for _, i := range *sl {
        out = out + i.Service.Hostname + ":" + i.Endpoint.Address + ":" + fmt.Sprintf("%d", i.Endpoint.Port) +  ", "
    }
    out = out + "]"
    return out
}

// NewMeshResourceView creates an aggregated store for resources sourced from various registries
func NewMeshResourceView() *MeshResourceView {
	return &MeshResourceView{
		registries:            make([]Registry, 0),
		mu:                    sync.RWMutex{},
		services:              make(map[resourceKey]*model.Service),
		serviceInstances:      make(map[resourceKey]*model.ServiceInstance),
		serviceLabels:         make(nameValueKeysMap),
		serviceInstanceLabels: make(nameValueKeysMap),
	}
}

// buildServiceKey builds a key to a service for a specific registry
// Format for service: [service name][platformRegistry]
// At the moment, there can only be one service object per platformRegistry
// TODO Upcoming PRs under #1223 plan to accommodate multi-cluster multi-cloud
// where the platform will be replaced by the name of the cluster
func buildServiceKey(r *Registry, s *model.Service) resourceKey {
	return resourceKey("[" + s.Hostname + "][" + string(r.Name) + "]")
}

func buildServiceKeyMap(r *Registry, services []*model.Service) map[resourceKey]*model.Service {
    out := map[resourceKey]*model.Service{}
    for _, svc := range services {
        out[buildServiceKey(r, svc)] = svc
    }
    return out
}

// buildServiceInstanceKey builds a key to a service instance for a specific registry
// Format for service instance: [service name][hex value of IP address][hex value of port number]
// Within the mesh there can be exactly one endpoint for a service with the combination of
// IP address and port.
func buildServiceInstanceKey(i *model.ServiceInstance) resourceKey {
	return resourceKey("[" + i.Service.Hostname + "][" + getIPHex(i.Endpoint.Address) + "][" + getPortHex(i.Endpoint.Port) + "]")
}

func buildServiceInstanceKeyMap(serviceInstances []*model.ServiceInstance) map[resourceKey]*model.ServiceInstance {
    out := map[resourceKey]*model.ServiceInstance{}
    for _, inst := range serviceInstances {
        out[buildServiceInstanceKey(inst)] = inst
    }
    return out
}

func getIPHex(address string) string {
	ip := net.ParseIP(address)
	return hex.EncodeToString(ip)
}

func getPortHex(port int) string {
	pb := []byte{byte((port >> 8) & 0xFF), byte(port & 0xFF)}
	return hex.EncodeToString(pb)
}

// A few labels are treated differently to ensure
// compatibility between various numeric and IP text
// values that are otherwise identical, ex: 08880 and 8080
// or 10.1.1.3 and ::ffff:10.1.1.3
func labelForNameValue(label string, value *string) resourceLabel {
	switch label {
	case labelServiceVIP:
		fallthrough
	case labelInstanceIP:
		ipHex := getIPHex(*value)
		return resourceLabel{label, &ipHex}
	case labelInstancePort:
		var port int
		fmt.Sscanf(*value, "%d", &port)
		portHex := getPortHex(port)
		return resourceLabel{label, &portHex}
	}
	return resourceLabel{label, value}
}

func labelsFromModel(lc model.Labels) resourceLabels {
	rl := make(resourceLabels, len(lc))
	i := 0
	for k := range lc {
		// make a copy to ensure each label
		// has a diff string address
		val := new(string)
		*val = lc[k]
		rl[i] = labelForNameValue(k, val)
		i++
	}
	return rl
}

func labelsForNameValues(label string, values []string) resourceLabels {
	rl := make(resourceLabels, len(values))
	for idx := range values {
		// ensure distinct string addresses
		var v *string
		v = &values[idx]
		rl[idx] = labelForNameValue(label, v)
	}
	return rl
}

// Implements ControllerViewHandler interface
func (r *Registry) Reconcile(cv *model.ControllerView) {
    // Delegate directly to the MeshView
    r.MeshView.reconcile(r, cv)
}

// AddRegistry adds registries into the aggregated MeshResourceView
func (v *MeshResourceView) AddRegistry(registry Registry) {
	// Create bidirectional associations between the MeshView and
	// the registry being added
	registry.MeshView = v
	v.registries = append(v.registries, registry)
	cvHandler := model.ControllerViewHandler(&registry)
	registry.Controller.Handle("/" + string(registry.Name) + "/", &cvHandler)
}

// Services lists services from all platforms
func (v *MeshResourceView) Services() ([]*model.Service, error) {
	lbls := resourceLabelsForName(labelServiceName)
	return v.serviceByLabels(lbls), nil
}

// GetService retrieves a service by hostname if exists
func (v *MeshResourceView) GetService(hostname string) (*model.Service, error) {
	lbls := labelForNameValue(labelServiceName, &hostname)
	svcs := v.serviceByLabels(resourceLabels{lbls})
	if len(svcs) > 0 {
		return svcs[0], nil
	}
	return nil, nil
}

// ManagementPorts retrieves set of health check ports by instance IP
// Return on the first hit.
func (v *MeshResourceView) ManagementPorts(addr string) model.PortList {
	lbls := labelsForIPSet(labelInstanceIP, map[string]bool{addr: true})
	instances := v.serviceInstancesByLabels(lbls)
	if len(instances) == 0 {
		return nil
	}
	portMap := map[int]*model.Port{}
	for _, inst := range instances {
		for _, mgmtPort := range inst.ManagementPorts {
			portMap[mgmtPort.Port] = mgmtPort
		}
	}
	if len(portMap) == 0 {
		return nil
	}
	out := make(model.PortList, len(portMap))
	pidx := 0
	for _, port := range portMap {
		out[pidx] = port
		pidx++
	}
	return out
}

// Instances retrieves instances for a service and its ports that match
// any of the supplied labels. All instances match an empty label list.
func (v *MeshResourceView) Instances(hostname string, ports []string,
	labels model.LabelsCollection) ([]*model.ServiceInstance, error) {
	hostPortLbls := labelsForNameValues(labelInstancePort, ports)
	hostPortLbls.appendNameValue(labelServiceName, hostname)
	if len(labels) > 0 {
		for _, lblset := range labels {
			lbls := labelsFromModel(lblset)
			lbls = append(lbls, hostPortLbls...)
			out := v.serviceInstancesByLabels(lbls)
			if len(out) > 0 {
				return out, nil
			}
		}
		return nil, nil
	}

	return v.serviceInstancesByLabels(hostPortLbls), nil
}

func labelsForIPSet(name string, values map[string]bool) resourceLabels {
	rl := make(resourceLabels, len(values))
	i := 0
	for v := range values {
		ipHex := getIPHex(v)
		rl[i] = resourceLabel{name, &ipHex}
		i++
	}
	return rl
}

// HostInstances lists service instances for a given set of IPv4 addresses.
func (v *MeshResourceView) HostInstances(addrs map[string]bool) ([]*model.ServiceInstance, error) {
	lbls := labelsForIPSet(labelInstanceIP, addrs)
	return v.serviceInstancesByLabels(lbls), nil
}

// Run starts all the MeshResourceViews
func (v *MeshResourceView) Run(stop <-chan struct{}) {

	for _, r := range v.registries {
		go r.Run(stop)
	}

	<-stop
	glog.V(2).Info("Registry Aggregator terminated")
}

// Implements model.UpdateNotifier
func (v *MeshResourceView) SetCacheEvictionHandler(h model.CacheEvictionHandler) error {
    if v.cacheEvictionHandler != nil {
		logMsg := "Fail to set cache eviction handler to aggregated mesh view. Maximum number of handlers '1' already added."
		glog.V(2).Info(logMsg)
		return errors.New(logMsg)
    }
    v.cacheEvictionHandler = h
    return nil
}

// GetIstioServiceAccounts implements model.ServiceAccounts operation
func (v *MeshResourceView) GetIstioServiceAccounts(hostname string, ports []string) []string {
	hostLabel := labelForNameValue(labelServiceName, &hostname)
	hostPortLbls := labelsForNameValues(labelInstancePort, ports)
	hostPortLbls.appendFrom(resourceLabels{hostLabel})
	instances := v.serviceInstancesByLabels(hostPortLbls)
	saSet := make(map[string]bool)
	for _, si := range instances {
		if si.ServiceAccount != "" {
			saSet[si.ServiceAccount] = true
		}
	}
	svcs := v.serviceByLabels(resourceLabels{hostLabel})
	for _, svc := range svcs {
		for _, serviceAccount := range svc.ServiceAccounts {
			saSet[serviceAccount] = true
		}
	}
	saArray := make([]string, 0, len(saSet))
	for sa := range saSet {
		saArray = append(saArray, sa)
	}
	return saArray
}

// Delegated implementation for ControllerViewHandler scoped to the registry
func (v *MeshResourceView) reconcile(r *Registry, cv *model.ControllerView) {
    rl := resourceLabels{}
    rl.appendNameValue(labelControllerPath, cv.Path)
    v.reconcileServices(r, cv.Path, rl, cv.Services)
    v.reconcileServiceInstances(cv.Path, rl, cv.ServiceInstances)
}

func (v *MeshResourceView) reconcileServices(r *Registry, cvp string, rl resourceLabels, expectedServices []*model.Service) {
    actualServices := v.serviceByLabels(rl)
    if glog.V(2) {
        glog.Infof("Expected: %s Actual %s", 
            ((*serviceList)(&expectedServices)).String(),
            ((*serviceList)(&actualServices)).String())
    }
    actualKeySvcMap := buildServiceKeyMap(r, actualServices)
    expectedKeySvcMap := buildServiceKeyMap(r, expectedServices)
    updateSet := map[resourceKey]*model.Service{}
    for k, expSvc := range expectedKeySvcMap {
        actSvc, found := actualKeySvcMap[k]
        if !found {
            continue  // Needs to be added to MeshView
        }
        if isServiceModified(expSvc, actSvc) {
            updateSet[k] = expSvc
        }
        // Remaining would be ones that need adding
        delete(expectedKeySvcMap, k)
        // Remaining would be ones that need deleting
        delete(actualKeySvcMap, k)
    }
    cacheReferences := model.CacheReferences{
        Kind: labelServiceName,
        Keyset: map[string]bool{}, 
    } 
    // Scope locks to performing actual changes.
    func () {
        v.mu.Lock()
        defer v.mu.Unlock()
        for k, delSvc := range actualKeySvcMap {
            v.reconcileService(cvp, k, delSvc, model.EventDelete)
            cacheReferences.Keyset[delSvc.Hostname] = true
        }    
        for k, addSvc := range expectedKeySvcMap {
            v.reconcileService(cvp, k, addSvc, model.EventAdd)
            cacheReferences.Keyset[addSvc.Hostname] = true
        }    
        for k, updSvc := range updateSet {
            v.reconcileService(cvp, k, updSvc, model.EventUpdate)
            cacheReferences.Keyset[updSvc.Hostname] = true
        }
    }()
    // Caches must be updated outside scope of locks
    v.cacheEvictionHandler.EvictCache(cacheReferences)
}

func (v *MeshResourceView) reconcileServiceInstances(cvp string, rl resourceLabels, expectedInstances []*model.ServiceInstance) {
    actualInstances := v.serviceInstancesByLabels(rl)
    actualKeyInstMap := buildServiceInstanceKeyMap(actualInstances)
    expectedKeyInstMap := buildServiceInstanceKeyMap(expectedInstances)
    updateSet := map[resourceKey]*model.ServiceInstance{}
    for k, expInst := range expectedKeyInstMap {
        actInst, found := actualKeyInstMap[k]
        if !found {
            continue  // Needs to be added to MeshView
        }
        if isServiceInstanceModified(expInst, actInst) {
            updateSet[k] = expInst
        }
        // Remaining would be ones that need adding
        delete(expectedKeyInstMap, k)
        // Remaining would be ones that need deleting
        delete(actualKeyInstMap, k)
    }
    cacheReferences := model.CacheReferences{
        Kind: labelInstanceRef,
        Keyset: map[string]bool{}, 
    } 
    // Scope locks to performing actual changes.
    func () {
    	v.mu.Lock()
    	defer v.mu.Unlock()
    	for k, delInst := range actualKeyInstMap {
            v.reconcileServiceInstance(cvp, k, delInst, model.EventDelete)
            cacheReferences.Keyset[k.String()] = true
    	}    
    	for k, addInst := range expectedKeyInstMap {
            v.reconcileServiceInstance(cvp, k, addInst, model.EventAdd)
            cacheReferences.Keyset[k.String()] = true
    	}    
    	for k, updInst := range updateSet {
            v.reconcileServiceInstance(cvp, k, updInst, model.EventUpdate)
            cacheReferences.Keyset[k.String()] = true
    	}    
    }()
    // Caches must be updated outside scope of locks
    v.cacheEvictionHandler.EvictCache(cacheReferences)
}

func getUniqueSet(values []string) map[string]bool {  
    out := map[string]bool{}
    for _, val := range values {
        out[val] = true
    }
    return out
}

func isSetModified(first, second map[string]bool) bool {
    for firstVal := range first {
        _, found := second[firstVal]
        if !found {
            return true
        }
    }
    return false
}

func isPortListModified(first, second model.PortList) bool {
    for idx, portFirst := range first {
        portSecond := second[idx]
        if portFirst.Name != portSecond.Name ||
            portFirst.Port != portSecond.Port ||
            portFirst.Protocol != portSecond.Protocol ||
            portFirst.AuthenticationPolicy != portSecond.AuthenticationPolicy {
                return true
            }
    }
    return false
}

// Compares services based on only what's needed for xDS to work.
func isServiceModified(expected, actual *model.Service) bool {
    // Skip checking host name, cause that would result a different resourceKey and this
    // function would never be invoked.
    if expected.Address != actual.Address ||
        expected.External() != actual.External() ||
        expected.ExternalName != actual.ExternalName ||
        expected.LoadBalancingDisabled != actual.LoadBalancingDisabled ||
        len(expected.ServiceAccounts) != len(actual.ServiceAccounts) ||
        len(expected.Ports) != len(actual.Ports) ||
        isPortListModified(expected.Ports, actual.Ports) ||
        isSetModified(getUniqueSet(expected.ServiceAccounts), getUniqueSet(actual.ServiceAccounts)) { 
        if glog.V(2) {
            glog.Infof("Service changed: Expected '%v' Actual '%v'", expected, actual)
        }    
        return true
    }
    return false
} 

// Compares services based on only what's needed for xDS to work.
func isServiceInstanceModified(expected, actual *model.ServiceInstance) bool {
    // Skip Endpoint and Service, cause that would result in a different resourceKey and this
    // function would never be invoked.
    if expected.AvailabilityZone != actual.AvailabilityZone ||
      expected.ServiceAccount != actual.ServiceAccount ||
      len(expected.Labels) != len(actual.Labels) ||
      len(expected.ManagementPorts) != len(actual.ManagementPorts) ||
      isPortListModified(expected.ManagementPorts, actual.ManagementPorts) ||
      !expected.Labels.Equals(actual.Labels) {
          return true
    }
    return false
}

// reconcileService is expected to be called only from inside reconcileServices(). The caller is expected
// to lock the resource view before calling this method()
func (v *MeshResourceView) reconcileService(cp string, k resourceKey, s *model.Service, e model.Event) {
    if glog.V(2) {
        glog.Infof("Svc event '%s' for service '%s'", e.String(), s.Hostname)
    }
	old, found := v.services[k]
	if found {
		v.serviceLabels.deleteLabel(k, labelControllerPath, cp)
		v.serviceLabels.deleteLabel(k, labelServiceName, old.Hostname)
		if old.ExternalName != "" {
			v.serviceLabels.deleteLabel(k, labelServiceExternalName, old.ExternalName)
		}
		if old.Address != "" {
			v.serviceLabels.deleteLabel(k, labelServiceVIP, getIPHex(old.Address))
		}
		delete(v.services, k)
	}
	if e != model.EventDelete {
		// Treat as upsert
		v.serviceLabels.addLabel(k, labelControllerPath, cp)
		v.serviceLabels.addLabel(k, labelServiceName, s.Hostname)
		if s.ExternalName != "" {
			v.serviceLabels.addLabel(k, labelServiceExternalName, s.ExternalName)
		}
		if s.Address != "" {
			v.serviceLabels.addLabel(k, labelServiceVIP, getIPHex(s.Address))
		}
		v.services[k] = s
	}
}

// reconcileServiceInstance is expected to be called only from inside reconcileServiceInstances(). The caller is expected
// to lock the resource view before calling this method()
func (v *MeshResourceView) reconcileServiceInstance(cp string, k resourceKey, i *model.ServiceInstance, e model.Event) {
	old, found := v.serviceInstances[k]
	if found {
		v.serviceInstanceLabels.deleteLabel(k, labelControllerPath, cp)
		v.serviceInstanceLabels.deleteLabel(k, labelServiceName, old.Service.Hostname)
		v.serviceInstanceLabels.deleteLabel(k, labelInstanceIP, getIPHex(old.Endpoint.Address))
		v.serviceInstanceLabels.deleteLabel(k, labelInstancePort, getPortHex(old.Endpoint.Port))
		if old.Endpoint.ServicePort.Name != "" {
			v.serviceInstanceLabels.deleteLabel(k, labelInstanceNamedPort, old.Endpoint.ServicePort.Name)
		}
		for label, value := range old.Labels {
			v.serviceInstanceLabels.deleteLabel(k, label, value)
		}
		delete(v.serviceInstances, k)
	}
	if e != model.EventDelete  {
		// Treat as upsert
		v.serviceInstanceLabels.addLabel(k, labelControllerPath, cp)
		v.serviceInstanceLabels.addLabel(k, labelServiceName, i.Service.Hostname)
		v.serviceInstanceLabels.addLabel(k, labelInstanceIP, getIPHex(i.Endpoint.Address))
		v.serviceInstanceLabels.addLabel(k, labelInstancePort, getPortHex(i.Endpoint.Port))
		if i.Endpoint.ServicePort != nil && i.Endpoint.ServicePort.Name != "" {
			v.serviceInstanceLabels.addLabel(k, labelInstanceNamedPort, i.Endpoint.ServicePort.Name)
		}
		for label, value := range i.Labels {
			v.serviceInstanceLabels.addLabel(k, label, value)
		}
		v.serviceInstances[k] = i
	}
}

func (v *MeshResourceView) serviceByLabels(labels resourceLabels) []*model.Service {
	v.mu.RLock()
	defer v.mu.RUnlock()
	svcKeySet := v.serviceLabels.getResourceKeysMatching(labels)
	out := make([]*model.Service, len(svcKeySet))
	i := 0
	for k := range svcKeySet {
		out[i] = v.services[k]
		i++
	}
	return out
}

func (v *MeshResourceView) serviceInstancesByLabels(labels resourceLabels) []*model.ServiceInstance {
	v.mu.RLock()
	defer v.mu.RUnlock()
	instanceKeySet := v.serviceInstanceLabels.getResourceKeysMatching(labels)
	out := make([]*model.ServiceInstance, len(instanceKeySet))
	i := 0
	for k := range instanceKeySet {
		out[i] = v.serviceInstances[k]
		i++
	}
	return out
}
