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
	"net"
	"reflect"
	"time"

	"github.com/golang/glog"
	"github.com/golang/protobuf/proto"
	"istio.io/manager/model"
)

// Watcher observes service registry and triggers a reload on a change
type Watcher interface {
}

type watcher struct {
	agent     Agent
	discovery model.ServiceDiscovery
	registry  *model.IstioRegistry
	mesh      *MeshConfig
	addrs     map[string]bool
}

// NewWatcher creates a new watcher instance with an agent
func NewWatcher(discovery model.ServiceDiscovery, ctl model.Controller,
	registry *model.IstioRegistry, mesh *MeshConfig) (Watcher, error) {
	addrs, err := hostIP()
	glog.V(2).Infof("host IPs: %v", addrs)
	if err != nil {
		return nil, err
	}

	out := &watcher{
		agent:     NewAgent(mesh.BinaryPath, mesh.ConfigPath),
		discovery: discovery,
		registry:  registry,
		mesh:      mesh,
		addrs:     addrs,
	}

	if err = ctl.AppendServiceHandler(func(*model.Service, model.Event) { out.reload() }); err != nil {
		return nil, err
	}

	// TODO: restrict the notification callback to co-located instances (e.g. with the same IP)
	if err = ctl.AppendInstanceHandler(func(*model.ServiceInstance, model.Event) { out.reload() }); err != nil {
		return nil, err
	}

	handler := func(model.Key, proto.Message, model.Event) { out.reload() }

	if err = ctl.AppendConfigHandler(model.RouteRule, handler); err != nil {
		return nil, err
	}

	if err = ctl.AppendConfigHandler(model.Destination, handler); err != nil {
		return nil, err
	}

	return out, nil
}

func (w *watcher) reload() {
	// FIXME: namespace?
	config := Generate(w.discovery.HostInstances(w.addrs), w.discovery.Services(),
		w.registry, w.mesh)
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

// hostIP returns IPv4 non-loopback host addresses
func hostIP() (map[string]bool, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, 0)
	for _, addr := range addrs {
		if ip, ok := addr.(*net.IPNet); ok {
			if !ip.IP.IsLoopback() && ip.IP.To4() != nil {
				out[ip.IP.String()] = true
			}
		}
	}
	return out, nil
}
