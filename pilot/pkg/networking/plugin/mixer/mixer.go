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

package mixer

import (
	"errors"

	xdsapi "github.com/envoyproxy/go-control-plane/envoy/api/v2"
	"github.com/envoyproxy/go-control-plane/envoy/api/v2/listener"
	http_conn "github.com/envoyproxy/go-control-plane/envoy/config/filter/network/http_connection_manager/v2"

	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking/plugin"
	"istio.io/istio/pilot/pkg/networking/util"
	"istio.io/istio/pilot/pkg/proxy/envoy/v1"
)

// Plugin is a mixer plugin.
type Plugin struct{}

// OnOutboundListener implements the Callbacks interface method.
func (m *Plugin) OnOutboundListener(in *plugin.CallbackListenerInputParams, mutable *plugin.CallbackListenerMutableObjects) error {
	if in.ServiceInstance == nil || in.ServiceInstance.Service == nil {
		return errors.New("mixer.Plugin#OnOutboundListener: in.ServiceInstance.Service must be non-nil")
	}
	if !in.ServiceInstance.Service.MeshExternal {
		return nil
	}

	env := in.Env
	node := in.Node
	proxyInstances := in.ProxyInstances

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		mutable.HTTPFilters = append(mutable.HTTPFilters, buildMixerHTTPFilter(env, node, proxyInstances, true))
	case plugin.ListenerTypeTCP:
		mutable.TCPFilters = append(mutable.TCPFilters, buildMixerOutboundTCPFilter(env, node))
	}

	return nil
}

// OnInboundListener implements the Callbacks interface method.
func (m *Plugin) OnInboundListener(in *plugin.CallbackListenerInputParams, mutable *plugin.CallbackListenerMutableObjects) error {
	env := in.Env
	node := in.Node
	proxyInstances := in.ProxyInstances
	instance := in.ServiceInstance

	switch in.ListenerType {
	case plugin.ListenerTypeHTTP:
		mutable.HTTPFilters = append(mutable.HTTPFilters, buildMixerHTTPFilter(env, node, proxyInstances, false))

	case plugin.ListenerTypeTCP:
		mutable.TCPFilters = append(mutable.TCPFilters, buildMixerInboundTCPFilter(env, node, instance))
	}

	return nil
}

// OnOutboundCluster implements the Callbacks interface method.
func (m *Plugin) OnOutboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnInboundCluster implements the Callbacks interface method.
func (m *Plugin) OnInboundCluster(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port, cluster *xdsapi.Cluster) {
}

// OnOutboundRoute implements the Callbacks interface method.
func (m *Plugin) OnOutboundRoute(env model.Environment, node model.Proxy, route *xdsapi.RouteConfiguration) {
}

// OnInboundRoute implements the Callbacks interface method.
func (m *Plugin) OnInboundRoute(env model.Environment, node model.Proxy, service *model.Service, servicePort *model.Port,
	route *xdsapi.RouteConfiguration) {
}

// buildMixerHTTPFilter builds a filter with a v1 mixer config encapsulated as JSON in a proto.Struct for v2 consumption.
func buildMixerHTTPFilter(env *model.Environment, node *model.Proxy,
	proxyInstances []*model.ServiceInstance, outbound bool) *http_conn.HttpFilter {
	mesh := env.Mesh
	config := env.IstioConfigStore
	if mesh.MixerCheckServer == "" && mesh.MixerReportServer == "" {
		return &http_conn.HttpFilter{}
	}

	c := v1.BuildHTTPMixerFilterConfig(mesh, *node, proxyInstances, outbound, config)
	return &http_conn.HttpFilter{
		Name:   v1.MixerFilter,
		Config: util.BuildProtoStruct(*c),
	}
}

// buildMixerInboundTCPFilter builds a filter with a v1 mixer config encapsulated as JSON in a proto.Struct for v2 consumption.
func buildMixerInboundTCPFilter(env *model.Environment, node *model.Proxy, instance *model.ServiceInstance) listener.Filter {
	mesh := env.Mesh
	if mesh.MixerCheckServer == "" && mesh.MixerReportServer == "" {
		return listener.Filter{}
	}

	c := v1.BuildTCPMixerFilterConfig(mesh, *node, instance)
	return listener.Filter{
		Config: util.BuildProtoStruct(*c),
	}
}

// buildMixerOutboundTCPFilter builds a filter with a v1 mixer config encapsulated as JSON in a proto.Struct for v2 consumption.
func buildMixerOutboundTCPFilter(env *model.Environment, node *model.Proxy) listener.Filter {
	// TODO(mostrowski): implementation
	return listener.Filter{}
}
