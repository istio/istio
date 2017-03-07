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

package envoy

import (
	"reflect"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"istio.io/manager/model"
)

// Watcher observes service registry and triggers a reload on a change
type Watcher interface {
}

// ProxyContext defines local proxy context information about the service mesh
type ProxyContext struct {
	// Discovery interface for listing services and instances
	Discovery model.ServiceDiscovery
	// Config interface for listing routing rules
	Config *model.IstioRegistry
	// MeshConfig defines global configuration settings
	MeshConfig *MeshConfig
	// Addrs is a set of IP addressed assigned to the proxy
	Addrs map[string]bool
}

// ProxyNode provides the local proxy node name and IP address
type ProxyNode struct {
	Name string
	IP   string
}

type watcher struct {
	agent   Agent
	context *ProxyContext
}

// NewWatcher creates a new watcher instance with an agent
func NewWatcher(discovery model.ServiceDiscovery, ctl model.Controller,
	registry *model.IstioRegistry, mesh *MeshConfig, identity *ProxyNode) (Watcher, error) {
	addrs := make(map[string]bool)
	if identity.IP != "" {
		addrs[identity.IP] = true
	}
	glog.V(2).Infof("Local instance address: %#v", addrs)

	// Use proxy node IP as the node name
	// This parameter is used as the value for "service-node"
	agent := NewAgent(mesh.BinaryPath, mesh.ConfigPath, identity.IP)

	out := &watcher{
		agent: agent,
		context: &ProxyContext{
			Discovery:  discovery,
			Config:     registry,
			MeshConfig: mesh,
			Addrs:      addrs,
		},
	}

	// Initialize envoy according to the current model state,
	// instead of waiting for the first event to arrive.
	// Note that this is currently done synchronously (blocking),
	// to avoid racing with controller events lurking around the corner.
	// This can be improved once we switch to a mechanism where reloads
	// are linearized (e.g., by a single goroutine reloader).
	out.reload()

	if err := ctl.AppendServiceHandler(func(*model.Service, model.Event) { out.reload() }); err != nil {
		return nil, err
	}

	// TODO: restrict the notification callback to co-located instances (e.g. with the same IP)
	// TODO: editing pod tags directly does not trigger instance handlers, we need to listen on pod resources.
	if err := ctl.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { out.reload() }); err != nil {
		return nil, err
	}

	handler := func(model.Key, proto.Message, model.Event) { out.reload() }

	if err := ctl.AppendConfigHandler(model.RouteRule, handler); err != nil {
		return nil, err
	}

	if err := ctl.AppendConfigHandler(model.DestinationPolicy, handler); err != nil {
		return nil, err
	}

	return out, nil
}

func (w *watcher) reload() {
	config := Generate(w.context)
	current := w.agent.ActiveConfig()

	if reflect.DeepEqual(config, current) {
		glog.V(2).Info("Configuration is identical, skipping reload")
		return
	}

	// TODO: add retry logic
	if err := w.agent.Reload(config); err != nil {
		glog.Warningf("Envoy reload error: %v", err)
		return
	}

	// Add a short delay to de-risk a potential race condition in envoy hot reload code.
	// The condition occurs when the active Envoy instance terminates in the middle of
	// the Reload() function.
	time.Sleep(256 * time.Millisecond)
}
