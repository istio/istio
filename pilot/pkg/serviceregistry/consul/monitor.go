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

package consul

import (
	"time"

	"github.com/hashicorp/consul/api"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/pkg/log"
)

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
	discovery        *api.Client
	instanceHandlers []InstanceHandler
	serviceHandlers  []ServiceHandler
}

const (
	refreshIdleTime    time.Duration = 5 * time.Second
	periodicCheckTime  time.Duration = 2 * time.Second
	blockQueryWaitTime time.Duration = 10 * time.Minute
)

// NewConsulMonitor watches for changes in Consul services and CatalogServices
func NewConsulMonitor(client *api.Client) Monitor {
	return &consulMonitor{
		discovery:        client,
		instanceHandlers: make([]InstanceHandler, 0),
		serviceHandlers:  make([]ServiceHandler, 0),
	}
}

func (m *consulMonitor) Start(stop <-chan struct{}) {
	change := make(chan struct{})
	go m.watchConsul(change, stop)
	go m.updateRecord(change, stop)
}

func (m *consulMonitor) watchConsul(change chan struct{}, stop <-chan struct{}) {
	var consulWaitIndex uint64

	for {
		select {
		case <-stop:
			return
		default:
			queryOptions := api.QueryOptions{
				WaitIndex: consulWaitIndex,
				WaitTime:  blockQueryWaitTime,
			}
			// This Consul REST API will block until service changes or timeout
			_, queryMeta, err := m.discovery.Catalog().Services(&queryOptions)
			if err != nil {
				log.Warnf("Could not fetch services: %v", err)
			} else if consulWaitIndex != queryMeta.LastIndex {
				consulWaitIndex = queryMeta.LastIndex
				change <- struct{}{}
			}
			time.Sleep(periodicCheckTime)
		}
	}
}

func (m *consulMonitor) updateRecord(change <-chan struct{}, stop <-chan struct{}) {
	lastChange := int64(0)
	ticker := time.NewTicker(periodicCheckTime)

	for {
		select {
		case <-change:
			lastChange = time.Now().Unix()
		case <-ticker.C:
			currentTime := time.Now().Unix()
			if lastChange > 0 && currentTime-lastChange > int64(refreshIdleTime.Seconds()) {
				log.Infof("Consul service changed")
				m.updateServiceRecord()
				m.updateInstanceRecord()
				lastChange = int64(0)
			}
		case <-stop:
			ticker.Stop()
			return
		}
	}
}

func (m *consulMonitor) updateServiceRecord() {
	// This is only a work-around solution currently
	// Since Handler functions generally act as a refresher
	// regardless of the input, thus passing in meaningless
	// input should make functionalities work
	//TODO
	var obj []*api.CatalogService
	var event model.Event
	for _, f := range m.serviceHandlers {
		go func(handler ServiceHandler) {
			if err := handler(obj, event); err != nil {
				log.Warnf("Error executing service handler function: %v", err)
			}
		}(f)
	}
}

func (m *consulMonitor) updateInstanceRecord() {
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
}

func (m *consulMonitor) AppendServiceHandler(h ServiceHandler) {
	m.serviceHandlers = append(m.serviceHandlers, h)
}

func (m *consulMonitor) AppendInstanceHandler(h InstanceHandler) {
	m.instanceHandlers = append(m.instanceHandlers, h)
}
