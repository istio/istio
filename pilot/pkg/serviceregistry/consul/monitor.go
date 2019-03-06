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

package consul

import (
	"reflect"
	"sort"
	"time"

	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pkg/log"
)

type consulServices map[string][]string
type consulServiceInstances []*api.CatalogService

// Monitor handles service and instance changes
type Monitor interface {
	Start(<-chan struct{})
	AppendServiceHandler(ServiceHandler)
	AppendInstanceHandler(InstanceHandler)
}

// InstanceHandler processes service instance change events
type InstanceHandler func(instance *api.CatalogService, event model.Event) error

// ServiceHandler processes service change events
type ServiceHandler func(instances []*api.CatalogService, event model.Event) error

type consulMonitor struct {
	discovery            *api.Client
	instanceCachedRecord consulServiceInstances
	serviceCachedRecord  consulServices
	instanceHandlers     []InstanceHandler
	serviceHandlers      []ServiceHandler
	period               time.Duration
}

// NewConsulMonitor polls for changes in Consul Services and CatalogServices
func NewConsulMonitor(client *api.Client, period time.Duration) Monitor {
	return &consulMonitor{
		discovery:            client,
		period:               period,
		instanceCachedRecord: make(consulServiceInstances, 0),
		serviceCachedRecord:  make(consulServices),
		instanceHandlers:     make([]InstanceHandler, 0),
		serviceHandlers:      make([]ServiceHandler, 0),
	}
}

func (m *consulMonitor) Start(stop <-chan struct{}) {
	m.run(stop)
}

func (m *consulMonitor) run(stop <-chan struct{}) {
	ticker := time.NewTicker(m.period)
	for {
		select {
		case <-stop:
			ticker.Stop()
			return
		case <-ticker.C:
			m.updateServiceRecord()
			m.updateInstanceRecord()
		}
	}
}

func (m *consulMonitor) updateServiceRecord() {
	svcs, _, err := m.discovery.Catalog().Services(nil)
	if err != nil {
		log.Warnf("Could not fetch services: %v", err)
		return
	}

	// The order of service tags may change even there is no service change
	// Sort the service tags to avoid unnecessary pushes to envoy
	for _, tags := range svcs {
		sort.Strings(tags)
	}
	newRecord := consulServices(svcs)
	if !reflect.DeepEqual(newRecord, m.serviceCachedRecord) {
		// This is only a work-around solution currently
		// Since Handler functions generally act as a refresher
		// regardless of the input, thus passing in meaningless
		// input should make functionalities work
		//TODO
		obj := []*api.CatalogService{}
		var event model.Event
		for _, f := range m.serviceHandlers {
			go func(handler ServiceHandler) {
				if err := handler(obj, event); err != nil {
					log.Warnf("Error executing service handler function: %v", err)
				}
			}(f)
		}
		m.serviceCachedRecord = newRecord
	}
}

func (m *consulMonitor) updateInstanceRecord() {
	svcs, _, err := m.discovery.Catalog().Services(nil)
	if err != nil {
		log.Warnf("Could not fetch instances: %v", err)
		return
	}

	instances := make([]*api.CatalogService, 0)
	for name := range svcs {
		endpoints, _, err := m.discovery.Catalog().Service(name, "", nil)
		if err != nil {
			log.Warnf("Could not retrieve service catalogue from consul: %v", err)
			continue
		}
		instances = append(instances, endpoints...)
	}

	newRecord := consulServiceInstances(instances)
	sort.Sort(newRecord)
	if !reflect.DeepEqual(newRecord, m.instanceCachedRecord) {
		// This is only a work-around solution currently
		// Since Handler functions generally act as a refresher
		// regardless of the input, thus passing in meaningless
		// input should make functionalities work
		// TODO
		obj := &api.CatalogService{}
		var event model.Event
		for _, f := range m.instanceHandlers {
			go func(handler InstanceHandler) {
				if err := handler(obj, event); err != nil {
					log.Warnf("Error executing instance handler function: %v", err)
				}
			}(f)
		}
		m.instanceCachedRecord = newRecord
	}
}

func (m *consulMonitor) AppendServiceHandler(h ServiceHandler) {
	m.serviceHandlers = append(m.serviceHandlers, h)
}

func (m *consulMonitor) AppendInstanceHandler(h InstanceHandler) {
	m.instanceHandlers = append(m.instanceHandlers, h)
}

// Len of the array
func (a consulServiceInstances) Len() int {
	return len(a)
}

// Swap i and j
func (a consulServiceInstances) Swap(i, j int) {
	a[i], a[j] = a[j], a[i]
}

// Less i and j
func (a consulServiceInstances) Less(i, j int) bool {
	// ID is the node ID
	// ServiceID is a unique service instance identifier
	return a[i].ID+a[i].ServiceID < a[j].ID+a[j].ServiceID
}
