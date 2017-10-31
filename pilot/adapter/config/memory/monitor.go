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

package memory

import (
	"github.com/golang/glog"

	"istio.io/istio/pilot/model"
)

const (
	// BufferSize specifies the buffer size of event channel
	BufferSize = 10
)

// Handler specifies a function to apply on a Config for a given event type
type Handler func(model.Config, model.Event)

// Monitor provides methods of manipulating changes in the config store
type Monitor interface {
	Run(<-chan struct{})
	AppendEventHandler(string, Handler)
	ScheduleProcessEvent(ConfigEvent)
}

// ConfigEvent defines the event to be processed
type ConfigEvent struct {
	config model.Config
	event  model.Event
}

type configstoreMonitor struct {
	store    model.ConfigStore
	handlers map[string][]Handler
	eventCh  chan ConfigEvent
}

// NewConfigStoreMonitor returns new Monitor implementation
func NewConfigStoreMonitor(store model.ConfigStore) Monitor {
	handlers := make(map[string][]Handler)

	for _, typ := range store.ConfigDescriptor().Types() {
		handlers[typ] = make([]Handler, 0)
	}

	return &configstoreMonitor{
		store:    store,
		handlers: handlers,
		eventCh:  make(chan ConfigEvent, BufferSize),
	}
}

func (m *configstoreMonitor) ScheduleProcessEvent(configEvent ConfigEvent) {
	m.eventCh <- configEvent
}

func (m *configstoreMonitor) Run(stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			if _, ok := <-m.eventCh; ok {
				close(m.eventCh)
			}
			return
		case ce, ok := <-m.eventCh:
			if ok {
				m.processConfigEvent(ce)
			}
		}
	}
}

func (m *configstoreMonitor) processConfigEvent(ce ConfigEvent) {
	if _, exists := m.handlers[ce.config.Type]; !exists {
		glog.Warningf("Config Type %s does not exist in config store", ce.config.Type)
		return
	}
	m.applyHandlers(ce.config, ce.event)
}

func (m *configstoreMonitor) AppendEventHandler(typ string, h Handler) {
	m.handlers[typ] = append(m.handlers[typ], h)
}

func (m *configstoreMonitor) applyHandlers(config model.Config, e model.Event) {
	for _, f := range m.handlers[config.Type] {
		f(config, e)
	}
}
