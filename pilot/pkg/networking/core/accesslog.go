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

package core

import (
	"sync"

	accesslog "github.com/envoyproxy/go-control-plane/envoy/config/accesslog/v3"
	core "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	listener "github.com/envoyproxy/go-control-plane/envoy/config/listener/v3"
	cel "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/filters/cel/v3"
	grpcaccesslog "github.com/envoyproxy/go-control-plane/envoy/extensions/access_loggers/grpc/v3"
	hcm "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/http_connection_manager/v3"
	tcp "github.com/envoyproxy/go-control-plane/envoy/extensions/filters/network/tcp_proxy/v3"

	meshconfig "istio.io/api/mesh/v1alpha1"
	"istio.io/istio/pilot/pkg/model"
	"istio.io/istio/pilot/pkg/networking"
	"istio.io/istio/pilot/pkg/util/protoconv"
	"istio.io/istio/pkg/wellknown"
)

const (
	// EnvoyServerName for istio's envoy
	EnvoyServerName = "istio-envoy"

	celFilter                          = "envoy.access_loggers.extension_filters.cel"
	listenerEnvoyAccessLogFriendlyName = "listener_envoy_accesslog"

	// EnvoyAccessLogCluster is the cluster name that has details for server implementing Envoy ALS.
	// This cluster is created in bootstrap.
	EnvoyAccessLogCluster = "envoy_accesslog_service"
)

var (
	// State logged by the metadata exchange filter about the upstream and downstream service instances
	// We need to propagate these as part of access log service stream
	// Logging them by default on the console may be an issue as the base64 encoded string is bound to be a big one.
	// But end users can certainly configure it on their own via the meshConfig using the %FILTER_STATE% macro.
	envoyWasmStateToLog = []string{"upstream_peer", "downstream_peer", // start from 1.24.0
		"wasm.upstream_peer", "wasm.upstream_peer_id", "wasm.downstream_peer", "wasm.downstream_peer_id"}

	// accessLogBuilder is used to set accessLog to filters
	accessLogBuilder = newAccessLogBuilder()
)

type AccessLogBuilder struct {
	// tcpGrpcAccessLog is used when access log service is enabled in mesh config.
	tcpGrpcAccessLog *accesslog.AccessLog
	// httpGrpcAccessLog is used when access log service is enabled in mesh config.
	httpGrpcAccessLog *accesslog.AccessLog
	// tcpGrpcListenerAccessLog is used when access log service is enabled in mesh config.
	tcpGrpcListenerAccessLog *accesslog.AccessLog

	coreAccessLog             cachedMeshConfigAccessLog
	listenerAccessLog         cachedMeshConfigAccessLog
	hboneOriginationAccessLog cachedMeshConfigAccessLog
	hboneTerminationAccessLog cachedMeshConfigAccessLog
}

func newAccessLogBuilder() *AccessLogBuilder {
	return &AccessLogBuilder{
		tcpGrpcAccessLog:         tcpGrpcAccessLog(false),
		httpGrpcAccessLog:        httpGrpcAccessLog(),
		tcpGrpcListenerAccessLog: tcpGrpcAccessLog(true),
		coreAccessLog:            newCachedMeshConfigAccessLog(nil),
		// We add ResponseFlagFilter here, as we want to get listener access logs only on scenarios where we might
		// not get filter Access Logs like in cases like NR to upstream.
		listenerAccessLog:         newCachedMeshConfigAccessLog(listenerAccessLogFilter()),
		hboneOriginationAccessLog: newCachedMeshConfigAccessLog(hboneOriginationAccessLogFilter()),
		hboneTerminationAccessLog: newCachedMeshConfigAccessLog(hboneTerminationAccessLogFilter()),
	}
}

func (b *AccessLogBuilder) setTCPAccessLogWithFilter(
	push *model.PushContext,
	proxy *model.Proxy,
	tcp *tcp.TcpProxy,
	class networking.ListenerClass,
	svc *model.Service,
	filter *accesslog.AccessLogFilter,
) {
	mesh := push.Mesh
	cfgs := push.Telemetry.AccessLogging(push, proxy, class, svc)

	if len(cfgs) == 0 {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			tcp.AccessLog = append(tcp.AccessLog, b.coreAccessLog.buildOrFetch(mesh, proxy))
		}

		if mesh.EnableEnvoyAccessLogService {
			// Setting it to TCP as the low level one.
			tcp.AccessLog = append(tcp.AccessLog, b.tcpGrpcAccessLog)
		}
		return
	}

	if al := buildAccessLogFromTelemetry(cfgs, filter); len(al) != 0 {
		tcp.AccessLog = append(tcp.AccessLog, al...)
	}
}

func (b *AccessLogBuilder) setTCPAccessLog(push *model.PushContext, proxy *model.Proxy, tcp *tcp.TcpProxy, class networking.ListenerClass, svc *model.Service) {
	b.setTCPAccessLogWithFilter(push, proxy, tcp, class, svc, nil)
}

func (b *AccessLogBuilder) setHboneOriginationAccessLog(push *model.PushContext, proxy *model.Proxy, tcp *tcp.TcpProxy, class networking.ListenerClass) {
	mesh := push.Mesh
	cfgs := push.Telemetry.AccessLogging(push, proxy, class, nil)

	if len(cfgs) == 0 {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			tcp.AccessLog = append(tcp.AccessLog, b.hboneOriginationAccessLog.buildOrFetch(mesh, proxy))
		}
		return
	}

	if al := buildAccessLogFromTelemetry(cfgs, hboneOriginationAccessLogFilter()); len(al) != 0 {
		tcp.AccessLog = append(tcp.AccessLog, al...)
	}
}

func (b *AccessLogBuilder) setHboneTerminationAccessLog(push *model.PushContext, proxy *model.Proxy,
	connectionManager *hcm.HttpConnectionManager, class networking.ListenerClass,
) {
	mesh := push.Mesh
	cfgs := push.Telemetry.AccessLogging(push, proxy, class, nil)
	if len(cfgs) == 0 {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			connectionManager.AccessLog = append(connectionManager.AccessLog, b.hboneTerminationAccessLog.buildOrFetch(mesh, proxy))
		}
		return
	}

	if al := buildAccessLogFromTelemetry(cfgs, hboneTerminationAccessLogFilter()); len(al) != 0 {
		connectionManager.AccessLog = append(connectionManager.AccessLog, al...)
	}
}

func buildAccessLogFromTelemetry(cfgs []model.LoggingConfig, filter *accesslog.AccessLogFilter) []*accesslog.AccessLog {
	als := make([]*accesslog.AccessLog, 0, len(cfgs))
	for _, c := range cfgs {
		if c.Disabled {
			continue
		}
		filters := make([]*accesslog.AccessLogFilter, 0, 2)
		if filter != nil {
			filters = append(filters, filter)
		}

		if telFilter := buildAccessLogFilterFromTelemetry(c); telFilter != nil {
			filters = append(filters, telFilter)
		}

		al := &accesslog.AccessLog{
			Name:       c.AccessLog.Name,
			ConfigType: c.AccessLog.ConfigType,
			Filter:     buildAccessLogFilter(filters...),
		}

		als = append(als, al)
	}
	return als
}

func buildAccessLogFilterFromTelemetry(spec model.LoggingConfig) *accesslog.AccessLogFilter {
	if spec.Filter == nil {
		return nil
	}

	fl := &cel.ExpressionFilter{
		Expression: spec.Filter.Expression,
	}

	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_ExtensionFilter{
			ExtensionFilter: &accesslog.ExtensionFilter{
				Name:       celFilter,
				ConfigType: &accesslog.ExtensionFilter_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
			},
		},
	}
}

func (b *AccessLogBuilder) setHTTPAccessLog(push *model.PushContext, proxy *model.Proxy,
	connectionManager *hcm.HttpConnectionManager, class networking.ListenerClass, svc *model.Service,
) {
	mesh := push.Mesh
	cfgs := push.Telemetry.AccessLogging(push, proxy, class, svc)
	if len(cfgs) == 0 {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			connectionManager.AccessLog = append(connectionManager.AccessLog, b.coreAccessLog.buildOrFetch(mesh, proxy))
		}

		if mesh.EnableEnvoyAccessLogService {
			connectionManager.AccessLog = append(connectionManager.AccessLog, b.httpGrpcAccessLog)
		}
		return
	}

	if al := buildAccessLogFromTelemetry(cfgs, nil); len(al) != 0 {
		connectionManager.AccessLog = append(connectionManager.AccessLog, al...)
	}
}

func (b *AccessLogBuilder) setListenerAccessLog(push *model.PushContext, proxy *model.Proxy,
	listener *listener.Listener, class networking.ListenerClass,
) {
	mesh := push.Mesh
	if mesh.DisableEnvoyListenerLog {
		return
	}

	cfgs := push.Telemetry.AccessLogging(push, proxy, class, nil)

	if len(cfgs) == 0 {
		// No Telemetry API configured, fall back to legacy mesh config setting
		if mesh.AccessLogFile != "" {
			listener.AccessLog = append(listener.AccessLog, b.listenerAccessLog.buildOrFetch(mesh, proxy))
		}

		if mesh.EnableEnvoyAccessLogService {
			// Setting it to TCP as the low level one.
			listener.AccessLog = append(listener.AccessLog, b.tcpGrpcListenerAccessLog)
		}

		return
	}

	if al := buildAccessLogFromTelemetry(cfgs, listenerAccessLogFilter()); len(al) != 0 {
		listener.AccessLog = append(listener.AccessLog, al...)
	}
}

func listenerAccessLogFilter() *accesslog.AccessLogFilter {
	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
			// NR: no filter chain found. This is triggered, typically, when the incoming TLS request doesn't match any filters
			ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"NR"}},
		},
	}
}

func hboneOriginationAccessLogFilter() *accesslog.AccessLogFilter {
	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_ResponseFlagFilter{
			// UF: upstream failure, we couldn't connect. This is important to log at this layer, since the error details
			// are lost otherwise.
			ResponseFlagFilter: &accesslog.ResponseFlagFilter{Flags: []string{"UF"}},
		},
	}
}

func hboneTerminationAccessLogFilter() *accesslog.AccessLogFilter {
	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_StatusCodeFilter{
			StatusCodeFilter: &accesslog.StatusCodeFilter{
				Comparison: &accesslog.ComparisonFilter{
					Op: accesslog.ComparisonFilter_GE,
					Value: &core.RuntimeUInt32{
						DefaultValue: 400,
						// Required by the API but useless for us. Set to a bogus value so we always use DefaultValue
						RuntimeKey: "istio.io/unset",
					},
				},
			},
		},
	}
}

func buildAccessLogFilter(f ...*accesslog.AccessLogFilter) *accesslog.AccessLogFilter {
	if len(f) == 0 {
		return nil
	}

	if len(f) == 1 {
		return f[0]
	}

	return &accesslog.AccessLogFilter{
		FilterSpecifier: &accesslog.AccessLogFilter_AndFilter{
			AndFilter: &accesslog.AndFilter{
				Filters: f,
			},
		},
	}
}

func newCachedMeshConfigAccessLog(filter *accesslog.AccessLogFilter) cachedMeshConfigAccessLog {
	return cachedMeshConfigAccessLog{filter: filter, cached: make(map[bool]*accesslog.AccessLog)}
}

type cachedMeshConfigAccessLog struct {
	filter *accesslog.AccessLogFilter
	mutex  sync.RWMutex
	cached map[bool]*accesslog.AccessLog
}

func (b *cachedMeshConfigAccessLog) getCached(skipBuiltInFormatter bool) *accesslog.AccessLog {
	b.mutex.RLock()
	defer b.mutex.RUnlock()
	return b.cached[skipBuiltInFormatter]
}

func (b *cachedMeshConfigAccessLog) buildOrFetch(mesh *meshconfig.MeshConfig, proxy *model.Proxy) *accesslog.AccessLog {
	// Skip built-in formatter if Istio version is >= 1.26
	// This is because if we send CEL/METADATA in the access log,
	// envoy will report lots of warn message endlessly.
	skipBuiltInFormatter := proxy.IstioVersion != nil && proxy.VersionGreaterOrEqual(&model.IstioVersion{Major: 1, Minor: 26})
	if c := b.getCached(skipBuiltInFormatter); c != nil {
		return c
	}
	// We need to build access log. This is needed either on first access or when mesh config changes.
	accessLog := model.FileAccessLogFromMeshConfig(mesh.AccessLogFile, mesh, skipBuiltInFormatter)
	accessLog.Filter = b.filter

	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.cached[skipBuiltInFormatter] = accessLog

	return accessLog
}

func (b *cachedMeshConfigAccessLog) reset() {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.cached = make(map[bool]*accesslog.AccessLog)
}

func tcpGrpcAccessLog(isListener bool) *accesslog.AccessLog {
	accessLogFriendlyName := model.TCPEnvoyAccessLogFriendlyName
	if isListener {
		accessLogFriendlyName = listenerEnvoyAccessLogFriendlyName
	}
	fl := &grpcaccesslog.TcpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: accessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: EnvoyAccessLogCluster,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
	}

	var filter *accesslog.AccessLogFilter
	if isListener {
		filter = listenerAccessLogFilter()
	}
	return &accesslog.AccessLog{
		Name:       model.TCPEnvoyALSName,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
		Filter:     filter,
	}
}

func httpGrpcAccessLog() *accesslog.AccessLog {
	fl := &grpcaccesslog.HttpGrpcAccessLogConfig{
		CommonConfig: &grpcaccesslog.CommonGrpcAccessLogConfig{
			LogName: model.HTTPEnvoyAccessLogFriendlyName,
			GrpcService: &core.GrpcService{
				TargetSpecifier: &core.GrpcService_EnvoyGrpc_{
					EnvoyGrpc: &core.GrpcService_EnvoyGrpc{
						ClusterName: EnvoyAccessLogCluster,
					},
				},
			},
			TransportApiVersion:     core.ApiVersion_V3,
			FilterStateObjectsToLog: envoyWasmStateToLog,
		},
	}

	return &accesslog.AccessLog{
		Name:       wellknown.HTTPGRPCAccessLog,
		ConfigType: &accesslog.AccessLog_TypedConfig{TypedConfig: protoconv.MessageToAny(fl)},
	}
}

func (b *AccessLogBuilder) reset() {
	b.coreAccessLog.reset()
	b.listenerAccessLog.reset()
	b.hboneOriginationAccessLog.reset()
	b.hboneTerminationAccessLog.reset()
}
